package main

import (
	"bytes"
	"fmt"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/instrumented"
	"github.com/clemuscle/symphony/internal/webhook"
	officialstacks "github.com/clemuscle/symphony/stacks"
)

// configSource lit la configuration : dans le repo Git désigné par le
// config_source du bootstrap local, sinon dans le fichier local lui-même
type configSource struct {
	desc  []any  // attributs de log
	label string // pour la page Administration
	read  func() ([]byte, error)
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
			desc:  []any{"source", path},
			label: path,
			read:  func() ([]byte, error) { return os.ReadFile(path) },
		}, nil
	}

	if src.Repo == "" {
		return nil, fmt.Errorf("config_source.repo requis dans %s", path)
	}
	if src.SCM.ID == "" {
		src.SCM.ID = "config-source"
	}
	if err := validateProviders(config.ProvidersConfig{Secrets: local.Providers.Secrets}); err != nil {
		return nil, fmt.Errorf("bootstrap %s: %w", path, err)
	}
	if err := validateDef("scm", src.SCM); err != nil {
		return nil, fmt.Errorf("config_source.scm: %w", err)
	}
	// Jeton du SCM : il peut venir des providers de secrets du bootstrap
	secrets := provider.NewRegistry()
	if err := registerSecrets(secrets, local.Providers.Secrets); err != nil {
		return nil, fmt.Errorf("bootstrap %s: %w", path, err)
	}
	scmDef, err := resolveDef(src.SCM, secrets.Secrets)
	if err != nil {
		return nil, fmt.Errorf("config_source.scm: %w", err)
	}
	scm, err := newSCM(scmDef)
	if err != nil {
		return nil, fmt.Errorf("config_source.scm: %w", err)
	}
	scm = instrumented.SCM(scm)
	repo, branch, file := src.Repo, orDefault(src.Branch, "main"), orDefault(src.Path, "symphony.yaml")
	return &configSource{
		desc:  []any{"bootstrap", path, "repo", repo, "branch", branch, "path", file},
		label: fmt.Sprintf("%s:%s (branche %s)", repo, file, branch),
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

// buildPolicy construit l'authentification et le CORS de la configuration
// (résolue) : connexion par le provider identity auth.identity (défaut : celui
// par défaut). Sans section auth, l'authentification est désactivée
// (développement local).
func buildPolicy(cfg *config.Config, providers *provider.Registry, codec *auth.Codec) (*auth.Policy, error) {
	p := &auth.Policy{CORSOrigins: cfg.CORSOrigins}
	if cfg.Auth == nil {
		slog.Warn("authentification désactivée (pas de section auth) : tout le monde est admin")
		return p, nil
	}
	idp, err := providers.Identity(cfg.Auth.Identity)
	if err != nil {
		return nil, fmt.Errorf("auth: provider identity: %w", err)
	}
	a, err := auth.NewAuthenticator(idp.OIDC(), cfg.Auth.Roles, codec)
	if err != nil {
		return nil, fmt.Errorf("auth (provider %s): %w", idp.ID(), err)
	}
	p.Auth = a
	slog.Info("authentification OIDC", "identity", idp.ID(), "type", idp.Type(), "issuer", idp.OIDC().Issuer, "client_id", idp.OIDC().ClientID)
	return p, nil
}

// catalogueFile : nom de la fiche catalogue dans le repo de chaque projet
func catalogueFile(cfg *config.Config) string {
	return orDefault(cfg.Catalogue.File, gitops.DefaultCatalogueFile)
}

// stacksSource : d'où lire les stacks (section stacks, ou l'ancienne section
// templates), avec leurs défauts, et ce que l'admin doit corriger. Une stack
// officielle inconnue est une erreur : la configuration est refusée.
func stacksSource(cfg *config.Config) (gitops.StackSource, []string, error) {
	switch {
	case cfg.Stacks != nil && cfg.Templates != nil:
		return gitops.StackSource{}, nil, fmt.Errorf("stacks et templates (son ancien nom) : n'en garder qu'une")
	case cfg.Templates != nil:
		return gitops.StackSource{
				Repo:   orDefault(cfg.Templates.Repo, "root/symphony-templates"),
				Branch: orDefault(cfg.Templates.Branch, "main"),
				Path:   orDefault(cfg.Templates.Path, "templates"),
			}, []string{"section templates : renommée stacks (l'ancien nom est lu jusqu'à la v1.0 ; " +
				"défauts de stacks : dossier stacks, et official pour le catalogue officiel)"}, nil
	case cfg.Stacks == nil:
		return gitops.StackSource{Repo: "root/symphony-templates", Branch: "main", Path: "templates"},
			[]string{"pas de section stacks : stacks lues dans root/symphony-templates, dossier templates " +
				"(ancien défaut, à déclarer : il disparaît à la v1.0)"}, nil
	}
	src := gitops.StackSource{
		Repo:     cfg.Stacks.Repo,
		Branch:   orDefault(cfg.Stacks.Branch, "main"),
		Path:     orDefault(cfg.Stacks.Path, "stacks"),
		Official: cfg.Stacks.Official,
	}
	available := gitops.OfficialNames(officialstacks.FS())
	for _, name := range src.Official {
		if name != "*" && !slices.Contains(available, name) {
			return src, nil, fmt.Errorf("stacks.official : %q inconnue (%s, ou * pour toutes)", name, strings.Join(available, ", "))
		}
	}
	var warnings []string
	if src.Repo == "" && len(src.Official) == 0 {
		warnings = append(warnings, "stacks : ni repo ni official, aucune stack n'est proposée")
	}
	return src, warnings, nil
}

// configWatcher relit la configuration périodiquement et l'applique à chaud :
// nouveaux providers, repos du catalogue et des stacks. Une configuration
// illisible ou invalide est refusée et la précédente reste en place.
// Le bootstrap local, lui, n'est relu qu'au redémarrage.
type configWatcher struct {
	source    *configSource
	providers *provider.Registry
	syncer    *gitops.Syncer
	stacks    *gitops.StackLoader
	security  *auth.Holder
	webhooks  *webhook.Holder
	envs      *environments.Holder
	codec     *auth.Codec
	interval  time.Duration

	last        []byte // dernier contenu traité (appliqué ou refusé)
	refused     bool   // le dernier contenu traité a été refusé
	lastErr     string // dernière erreur de lecture, pour ne la journaliser qu'une fois
	fingerprint string // empreinte de la configuration en place, secrets résolus
	secretErr   string // dernière erreur de relecture des secrets

	// resubscribe : appelé (en arrière-plan) quand l'URL ou le secret des
	// webhooks change, pour réabonner les repos des projets
	resubscribe func(*webhook.Settings)

	statusMu sync.Mutex
	status   config.Status
}

// Status : la configuration en place et ses derniers rechargements
func (w *configWatcher) Status() config.Status {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	return w.status
}

func (w *configWatcher) updateStatus(f func(*config.Status)) {
	w.statusMu.Lock()
	defer w.statusMu.Unlock()
	f(&w.status)
}

// applied note la configuration raw comme en place
func (w *configWatcher) applied(raw []byte, cfg *config.Config) {
	w.updateStatus(func(s *config.Status) {
		s.Source = w.source.label
		s.Revision = config.Revision(raw)
		s.AppliedAt = time.Now()
		s.CheckedAt = s.AppliedAt
		s.Refused = nil
		s.Types = cfg.ProviderTypes()
		_, s.Warnings, _ = stacksSource(cfg)
	})
}

func (w *configWatcher) Start() {
	ticker := time.NewTicker(w.interval)
	for range ticker.C {
		w.check()
	}
}

func (w *configWatcher) check() {
	cfg, raw, err := w.source.load()
	w.updateStatus(func(s *config.Status) {
		s.CheckedAt = time.Now()
		s.ReadError = ""
		if raw == nil {
			s.ReadError = err.Error()
		}
	})
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
			w.refuse(raw, err)
		} else {
			w.applied(raw, cfg)
		}
	} else if err == nil && !w.refused && bytes.Contains(raw, []byte(provider.SecretScheme)) {
		w.checkSecrets(cfg)
	}
	// Tant qu'une version refusée reste dans Git, la configuration en place
	// n'est pas la dernière : on continue de le compter
	if w.refused {
		metrics.Reload("config", "refused")
	} else {
		metrics.Reload("config", "ok")
	}
}

// checkSecrets relit les secrets de la configuration en place (inchangée) :
// si l'un d'eux a changé dans son provider (rotation), elle est réappliquée.
// Les providers de secrets en place servent à la relecture.
func (w *configWatcher) checkSecrets(cfg *config.Config) {
	resolved, err := resolveConfig(cfg, w.providers.Secrets)
	w.updateStatus(func(s *config.Status) {
		s.SecretsError = ""
		if err != nil {
			s.SecretsError = err.Error()
		}
	})
	if err != nil {
		if err.Error() != w.secretErr {
			w.secretErr = err.Error()
			slog.Warn("secrets illisibles, configuration en place conservée", "error", err)
		}
		return
	}
	w.secretErr = ""
	if fingerprint(resolved) == w.fingerprint {
		return
	}
	slog.Info("secret modifié dans son provider : configuration réappliquée")
	if err := w.apply(cfg); err != nil {
		slog.Error("configuration refusée après changement d'un secret, configuration en place conservée", "error", err)
		w.updateStatus(func(s *config.Status) {
			s.SecretsError = "secret modifié, configuration refusée : " + err.Error()
		})
		return
	}
	w.applied(w.last, cfg)
}

// refuse note la version raw comme refusée
func (w *configWatcher) refuse(raw []byte, err error) {
	w.updateStatus(func(s *config.Status) {
		s.Refused = &config.Refusal{Revision: config.Revision(raw), At: time.Now(), Error: err.Error()}
	})
}

// apply construit les nouveaux providers puis les met en place d'un bloc ;
// le catalogue et les stacks sont rechargés avec eux
func (w *configWatcher) apply(cfg *config.Config) error {
	stacks, _, err := stacksSource(cfg)
	if err != nil {
		return err
	}
	next, resolved, err := buildProviders(cfg)
	if err != nil {
		return err
	}
	policy, err := buildPolicy(resolved, next, w.codec)
	if err != nil {
		return err
	}
	hooks, err := buildWebhooks(resolved)
	if err != nil {
		return err
	}
	w.fingerprint = fingerprint(resolved)
	old := w.webhooks.Get()
	w.providers.Replace(next)
	w.security.Set(policy)
	w.webhooks.Set(hooks)
	if w.envs != nil {
		w.envs.Set(resolved.Environments)
	}
	if hooks.Enabled() && (old.URL != hooks.URL || old.Secret != hooks.Secret) && w.resubscribe != nil {
		go w.resubscribe(hooks)
	}
	w.syncer.SetFile(catalogueFile(cfg))
	w.stacks.SetSource(stacks)
	logProviders("configuration rechargée", w.providers)

	go w.syncer.ForceSync()
	go w.stacks.Reload()
	return nil
}

// buildWebhooks : réglages des webhooks entrants (configuration résolue).
// Sans section webhooks, les tâches suivent les pipelines par sondage. Le
// secret doit être assez long pour ne pas se deviner.
func buildWebhooks(cfg *config.Config) (*webhook.Settings, error) {
	if cfg.Webhooks == nil || cfg.Webhooks.URL == "" {
		return nil, nil
	}
	secret := cfg.Webhooks.Secret
	if len(secret) < 16 {
		return nil, fmt.Errorf("webhooks.secret : 16 caractères au moins requis (ex. secret://env/WEBHOOK_SECRET)")
	}
	return &webhook.Settings{
		URL:    cfg.Webhooks.URL,
		Secret: secret,
	}, nil
}
