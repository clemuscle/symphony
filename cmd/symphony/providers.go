package main

import (
	"fmt"
	"log/slog"
	"os"

	"github.com/yourorg/symphony/internal/config"
	"github.com/yourorg/symphony/internal/provider"
	githubci "github.com/yourorg/symphony/internal/provider/ci/githubactions"
	gitlabci "github.com/yourorg/symphony/internal/provider/ci/gitlabci"
	gitlabregistry "github.com/yourorg/symphony/internal/provider/registry/gitlabregistry"
	dockerrt "github.com/yourorg/symphony/internal/provider/runtime/docker"
	k8srt "github.com/yourorg/symphony/internal/provider/runtime/kubernetes"
	githubscm "github.com/yourorg/symphony/internal/provider/scm/github"
	gitlabscm "github.com/yourorg/symphony/internal/provider/scm/gitlab"
)

// buildProviders crée les providers de la configuration dans un nouveau
// registry. Un type inconnu est ignoré avec un warning ; sans provider SCM
// par défaut (catalogue et templates en dépendent), la configuration est refusée.
func buildProviders(cfg *config.Config) (*provider.Registry, error) {
	providers := provider.NewRegistry()

	for _, def := range cfg.Providers.SCM {
		p, err := newSCM(def)
		if err != nil {
			slog.Warn("provider SCM ignoré", "id", def.ID, "error", err)
			continue
		}
		providers.RegisterSCM(p, def.Default)
	}

	for _, def := range cfg.Providers.CI {
		switch def.Type {
		case "gitlab_ci":
			providers.RegisterCI(gitlabci.New(def.ID, def.Config["url"], gitlabToken(def)), def.Default)
		case "github_actions":
			providers.RegisterCI(githubci.New(def.ID, os.Getenv(def.Config["token_env"])), def.Default)
		default:
			slog.Warn("provider CI ignoré", "id", def.ID, "error", fmt.Sprintf("type %q inconnu", def.Type))
		}
	}

	for _, def := range cfg.Providers.Registry {
		switch def.Type {
		case "gitlab_registry":
			providers.RegisterRegistry(gitlabregistry.New(def.ID, def.Config["url"], def.Config["registry_host"], gitlabToken(def)), def.Default)
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
			providers.RegisterRuntime(dockerrt.New(def.ID, socket), def.Default)
		case "kubernetes":
			providers.RegisterRuntime(k8srt.New(def.ID), def.Default) // stub
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
