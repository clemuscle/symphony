package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/instrumented"
)

// configSource lit la configuration : dans le repo Git désigné par le
// config_source du bootstrap local, sinon dans le fichier local lui-même
type configSource struct {
	desc []any // attributs de log
	read func() ([]byte, error)
}

// newConfigSource lit le bootstrap local. S'il contient config_source, la
// configuration est lue dans Git avec un client SCM dédié (les providers ne
// sont pas encore connus à ce stade) ; le bootstrap n'est lu qu'une fois.
func newConfigSource(path string) (*configSource, error) {
	local, err := config.Load(path)
	if err != nil {
		return nil, err
	}
	src := local.ConfigSource
	if src == nil {
		return &configSource{
			desc: []any{"source", path},
			read: func() ([]byte, error) { return os.ReadFile(path) },
		}, nil
	}

	if src.Repo == "" {
		return nil, fmt.Errorf("config_source.repo requis dans %s", path)
	}
	if src.SCM.ID == "" {
		src.SCM.ID = "config-source"
	}
	scm, err := newSCM(src.SCM)
	if err != nil {
		return nil, fmt.Errorf("config_source.scm: %w", err)
	}
	scm = instrumented.SCM(scm)
	repo, branch, file := src.Repo, orDefault(src.Branch, "main"), orDefault(src.Path, "symphony.yaml")
	return &configSource{
		desc: []any{"bootstrap", path, "repo", repo, "branch", branch, "path", file},
		read: func() ([]byte, error) {
			content, err := scm.GetFileContent(repo, branch, file)
			if err != nil {
				return nil, fmt.Errorf("lecture de la configuration %s:%s dans %s: %w", file, branch, repo, err)
			}
			return []byte(content), nil
		},
	}, nil
}

// load lit et valide la configuration. raw (le contenu lu) est renvoyé même
// si la configuration est invalide, pour ne la signaler qu'une fois.
func (s *configSource) load() (cfg *config.Config, raw []byte, err error) {
	raw, err = s.read()
	if err != nil {
		return nil, nil, err
	}
	cfg, err = config.Parse(raw)
	if err != nil {
		return nil, raw, err
	}
	if cfg.ConfigSource != nil {
		return nil, raw, fmt.Errorf("config_source n'est autorisé que dans le bootstrap local")
	}
	if cfg.Catalogue.Repo == "" {
		return nil, raw, fmt.Errorf("catalogue.repo requis")
	}
	return cfg, raw, nil
}

// loadConfig lit le bootstrap puis la configuration qu'il désigne
func loadConfig(path string) (*config.Config, error) {
	src, err := newConfigSource(path)
	if err != nil {
		return nil, err
	}
	cfg, _, err := src.load()
	return cfg, err
}

// buildPolicy construit l'authentification et le CORS de la configuration.
// Sans section auth, l'authentification est désactivée (développement local).
func buildPolicy(cfg *config.Config, codec *auth.Codec) (*auth.Policy, error) {
	p := &auth.Policy{CORSOrigins: cfg.CORSOrigins}
	if cfg.Auth == nil {
		slog.Warn("authentification désactivée (pas de section auth) : tout le monde est admin")
		return p, nil
	}
	a, err := auth.NewAuthenticator(*cfg.Auth, codec)
	if err != nil {
		return nil, fmt.Errorf("auth: %w", err)
	}
	p.Auth = a
	slog.Info("authentification OIDC", "issuer", cfg.Auth.Issuer, "client_id", cfg.Auth.ClientID)
	return p, nil
}

// catalogueSource : repo, branche et dossier du catalogue, avec leurs défauts.
// CATALOGUE_BRANCH surcharge la branche de la configuration.
func catalogueSource(cfg *config.Config) (repo, branch, dir string) {
	return cfg.Catalogue.Repo,
		getEnv("CATALOGUE_BRANCH", orDefault(cfg.Catalogue.Branch, "main")),
		orDefault(cfg.Catalogue.Path, "services")
}

// templatesSource : repo, branche et dossier des templates, avec leurs défauts
func templatesSource(cfg *config.Config) (repo, branch, dir string) {
	return orDefault(cfg.Templates.Repo, "root/symphony-templates"),
		orDefault(cfg.Templates.Branch, "main"),
		orDefault(cfg.Templates.Path, "templates")
}

// configWatcher relit la configuration périodiquement et l'applique à chaud :
// nouveaux providers, repos du catalogue et des templates. Une configuration
// illisible ou invalide est refusée et la précédente reste en place.
// Le bootstrap local, lui, n'est relu qu'au redémarrage.
type configWatcher struct {
	source    *configSource
	providers *provider.Registry
	syncer    *gitops.Syncer
	templates *gitops.TemplateLoader
	security  *auth.Holder
	codec     *auth.Codec
	interval  time.Duration

	last    []byte // dernier contenu traité (appliqué ou refusé)
	refused bool   // le dernier contenu traité a été refusé
	lastErr string // dernière erreur de lecture, pour ne la journaliser qu'une fois
}

func (w *configWatcher) Start() {
	ticker := time.NewTicker(w.interval)
	for range ticker.C {
		w.check()
	}
}

func (w *configWatcher) check() {
	cfg, raw, err := w.source.load()
	if raw == nil {
		// Lecture impossible (GitLab injoignable…) : on réessaiera
		metrics.Reload("config", "error")
		if err.Error() != w.lastErr {
			w.lastErr = err.Error()
			slog.Warn("configuration illisible, configuration en place conservée", "error", err)
		}
		return
	}
	w.lastErr = ""

	if !bytes.Equal(raw, w.last) {
		w.last = raw
		if err == nil {
			err = w.apply(cfg)
		}
		w.refused = err != nil
		if err != nil {
			slog.Error("configuration refusée, configuration en place conservée", "error", err)
		}
	}
	// Tant qu'une version refusée reste dans Git, la configuration en place
	// n'est pas la dernière : on continue de le compter
	if w.refused {
		metrics.Reload("config", "refused")
	} else {
		metrics.Reload("config", "ok")
	}
}

// apply construit les nouveaux providers puis les met en place d'un bloc ;
// le catalogue et les golden paths sont rechargés avec eux
func (w *configWatcher) apply(cfg *config.Config) error {
	next, err := buildProviders(cfg)
	if err != nil {
		return err
	}
	policy, err := buildPolicy(cfg, w.codec)
	if err != nil {
		return err
	}
	w.providers.Replace(next)
	w.security.Set(policy)
	w.syncer.SetSource(catalogueSource(cfg))
	w.templates.SetSource(templatesSource(cfg))
	logProviders("configuration rechargée", w.providers)

	go w.syncer.ForceSync()
	go w.templates.Reload()
	return nil
}
