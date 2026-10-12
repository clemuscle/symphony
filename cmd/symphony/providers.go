package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/provider"
	githubci "github.com/clemuscle/symphony/internal/provider/ci/githubactions"
	gitlabci "github.com/clemuscle/symphony/internal/provider/ci/gitlabci"
	"github.com/clemuscle/symphony/internal/provider/instrumented"
	gitlabregistry "github.com/clemuscle/symphony/internal/provider/registry/gitlabregistry"
	argocdrt "github.com/clemuscle/symphony/internal/provider/runtime/argocd"
	dockerrt "github.com/clemuscle/symphony/internal/provider/runtime/docker"
	githubscm "github.com/clemuscle/symphony/internal/provider/scm/github"
	gitlabscm "github.com/clemuscle/symphony/internal/provider/scm/gitlab"
)

// buildProviders crée les providers de la configuration dans un nouveau
// registry, enveloppés pour mesurer leurs appels (/metrics). Un type inconnu est ignoré avec un warning ; sans provider SCM
// par défaut (catalogue et templates en dépendent), la configuration est refusée.
func buildProviders(cfg *config.Config) (*provider.Registry, error) {
	providers := provider.NewRegistry()

	for _, def := range cfg.Providers.SCM {
		p, err := newSCM(def)
		if err != nil {
			slog.Warn("provider SCM ignoré", "id", def.ID, "error", err)
			continue
		}
		providers.RegisterSCM(instrumented.SCM(p), def.Default)
	}

	for _, def := range cfg.Providers.CI {
		switch def.Type {
		case "gitlab_ci":
			providers.RegisterCI(instrumented.CI(gitlabci.New(def.ID, def.Config["url"], gitlabToken(def))), def.Default)
		case "github_actions":
			providers.RegisterCI(instrumented.CI(githubci.New(def.ID, os.Getenv(def.Config["token_env"]))), def.Default)
		default:
			slog.Warn("provider CI ignoré", "id", def.ID, "error", fmt.Sprintf("type %q inconnu", def.Type))
		}
	}

	for _, def := range cfg.Providers.Registry {
		switch def.Type {
		case "gitlab_registry":
			providers.RegisterRegistry(instrumented.Registry(gitlabregistry.New(def.ID, def.Config["url"], def.Config["registry_host"], gitlabToken(def))), def.Default)
		default:
			slog.Warn("provider registry ignoré", "id", def.ID, "error", fmt.Sprintf("type %q inconnu", def.Type))
		}
	}

	for _, def := range cfg.Providers.Runtime {
		switch def.Type {
		case "docker":
			socket := def.Config["socket"]
			if socket == "" {
				socket = "/var/run/docker.sock"
			}
			providers.RegisterRuntime(instrumented.Runtime(dockerrt.New(def.ID, socket)), def.Default)
		case "argocd":
			rt, err := newArgoCD(def, providers)
			if err != nil {
				slog.Warn("provider runtime ignoré", "id", def.ID, "error", err)
				continue
			}
			providers.RegisterRuntime(instrumented.Runtime(rt), def.Default)
		default:
			slog.Warn("provider runtime ignoré", "id", def.ID, "error", fmt.Sprintf("type %q inconnu", def.Type))
		}
	}

	if _, err := providers.SCM(""); err != nil {
		return nil, fmt.Errorf("pas de provider SCM par défaut: %w", err)
	}
	return providers, nil
}

// logProviders journalise les providers enregistrés
func logProviders(msg string, providers *provider.Registry) {
	slog.Info(msg,
		"scm", providers.ListSCM(),
		"ci", providers.ListCI(),
		"registry", providers.ListRegistries(),
		"runtime", providers.ListRuntimes())
}

// newSCM crée un provider SCM à partir de sa définition
func newSCM(def config.ProviderDef) (provider.SCMProvider, error) {
	switch def.Type {
	case "gitlab":
		token := os.Getenv(def.Config["token_env"])
		if token == "" {
			token = def.Config["token"]
		}
		return gitlabscm.New(def.ID, def.Config["url"], token), nil

	case "github":
		return githubscm.New(def.ID, os.Getenv(def.Config["token_env"]), def.Config["org"]), nil
	}
	return nil, fmt.Errorf("type de provider SCM %q inconnu", def.Type)
}

// newArgoCD crée le runtime Argo CD. Il écrit dans le repo de déploiement avec
// le SCM scm_ref (défaut : SCM par défaut) de la même configuration.
func newArgoCD(def config.ProviderDef, providers *provider.Registry) (*argocdrt.Provider, error) {
	scm, err := providers.SCM(def.Config["scm_ref"])
	if err != nil {
		return nil, err
	}
	tokenEnv := def.Config["token_env"]
	if tokenEnv == "" {
		tokenEnv = "ARGOCD_TOKEN"
	}
	c := def.Config
	return argocdrt.New(def.ID, scm, argocdrt.Config{
		URL:           c["url"],
		Token:         os.Getenv(tokenEnv),
		Insecure:      c["insecure"] == "true",
		Repo:          c["repo"],
		Branch:        c["branch"],
		Path:          c["path"],
		RepoURL:       c["repo_url"],
		Project:       c["project"],
		Cluster:       c["cluster"],
		Namespace:     c["namespace"],
		IngressDomain: c["ingress_domain"],
		IngressClass:  c["ingress_class"],
		IngressPort:   c["ingress_port"],
	})
}

// gitlabToken : token_env, sinon <scm_ref>_TOKEN, sinon GITLAB_TOKEN
// (réutilise le token du SCM GitLab référencé)
func gitlabToken(def config.ProviderDef) string {
	if token := os.Getenv(def.Config["token_env"]); token != "" {
		return token
	}
	if token := os.Getenv(def.Config["scm_ref"] + "_TOKEN"); token != "" {
		return token
	}
	return os.Getenv("GITLAB_TOKEN")
}
