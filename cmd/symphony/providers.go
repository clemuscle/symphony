package main

import (
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/provider"
	githubci "github.com/clemuscle/symphony/internal/provider/ci/githubactions"
	gitlabci "github.com/clemuscle/symphony/internal/provider/ci/gitlabci"
	keycloakidp "github.com/clemuscle/symphony/internal/provider/identity/keycloak"
	oidcidp "github.com/clemuscle/symphony/internal/provider/identity/oidc"
	"github.com/clemuscle/symphony/internal/provider/instrumented"
	"github.com/clemuscle/symphony/internal/provider/registry/ghcr"
	gitlabregistry "github.com/clemuscle/symphony/internal/provider/registry/gitlabregistry"
	argocdrt "github.com/clemuscle/symphony/internal/provider/runtime/argocd"
	dockerrt "github.com/clemuscle/symphony/internal/provider/runtime/docker"
	githubscm "github.com/clemuscle/symphony/internal/provider/scm/github"
	gitlabscm "github.com/clemuscle/symphony/internal/provider/scm/gitlab"
)

// buildProviders crée les providers de la configuration dans un nouveau
// registry, enveloppés pour mesurer leurs appels (/metrics). Les providers de
// secrets viennent d'abord : la configuration est résolue avec eux (secret://)
// et renvoyée résolue, pour la politique d'accès et les webhooks.
// Un type inconnu est ignoré avec un warning ; un secret introuvable, ou
// l'absence de provider SCM par défaut (catalogue et templates en dépendent),
// fait refuser la configuration.
func buildProviders(cfg *config.Config) (*provider.Registry, *config.Config, error) {
	providers := provider.NewRegistry()
	if err := registerSecrets(providers, cfg.Providers.Secrets); err != nil {
		return nil, nil, err
	}
	resolved, err := resolveConfig(legacyAuth(cfg, true), providers.Secrets)
	if err != nil {
		return nil, nil, err
	}

	for _, def := range resolved.Providers.SCM {
		p, err := newSCM(def)
		if err != nil {
			slog.Warn("provider SCM ignoré", "id", def.ID, "error", err)
			continue
		}
		providers.RegisterSCM(instrumented.SCM(p), def.Default)
	}

	for _, def := range resolved.Providers.CI {
		switch def.Type {
		case "gitlab_ci":
			providers.RegisterCI(instrumented.CI(gitlabci.New(def.ID, def.Config["url"], gitlabToken(def, resolved))), def.Default)
		case "github_actions":
			providers.RegisterCI(instrumented.CI(githubci.New(def.ID, scmToken(def, resolved, "GITHUB_TOKEN"))), def.Default)
		default:
			slog.Warn("provider CI ignoré", "id", def.ID, "error", fmt.Sprintf("type %q inconnu", def.Type))
		}
	}

	for _, def := range resolved.Providers.Registry {
		switch def.Type {
		case "gitlab_registry":
			providers.RegisterRegistry(instrumented.Registry(gitlabregistry.New(def.ID, def.Config["url"], def.Config["registry_host"], gitlabToken(def, resolved))), def.Default)
		case "ghcr":
			providers.RegisterRegistry(instrumented.Registry(ghcr.New(def.ID, def.Config["owner"], scmToken(def, resolved, "GITHUB_TOKEN"))), def.Default)
		default:
			slog.Warn("provider registry ignoré", "id", def.ID, "error", fmt.Sprintf("type %q inconnu", def.Type))
		}
	}

	for _, def := range resolved.Providers.Runtime {
		switch def.Type {
		case "docker":
			rt, err := newDocker(def)
			if err != nil {
				slog.Warn("provider runtime ignoré", "id", def.ID, "error", err)
				continue
			}
			providers.RegisterRuntime(instrumented.Runtime(rt), def.Default)
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

	for _, def := range resolved.Providers.Identity {
		p, err := newIdentity(def)
		if err != nil {
			slog.Warn("provider identity ignoré", "id", def.ID, "error", err)
			continue
		}
		providers.RegisterIdentity(instrumented.Identity(p), def.Default)
	}

	if _, err := providers.SCM(""); err != nil {
		return nil, nil, fmt.Errorf("pas de provider SCM par défaut: %w", err)
	}
	return providers, resolved, nil
}

// logProviders journalise les providers enregistrés
func logProviders(msg string, providers *provider.Registry) {
	slog.Info(msg,
		"scm", providers.ListSCM(),
		"ci", providers.ListCI(),
		"registry", providers.ListRegistries(),
		"runtime", providers.ListRuntimes(),
		"secrets", providers.ListSecrets(),
		"identity", providers.ListIdentity())
}

// newSCM crée un provider SCM à partir de sa définition
func newSCM(def config.ProviderDef) (provider.SCMProvider, error) {
	switch def.Type {
	case "gitlab":
		return gitlabscm.New(def.ID, def.Config["url"], secretValue(def, "token")).WithNamespace(def.Config["namespace"]), nil

	case "github":
		return githubscm.New(def.ID, secretValue(def, "token"), def.Config["org"]), nil
	}
	return nil, fmt.Errorf("type de provider SCM %q inconnu", def.Type)
}

// newDocker crée le runtime Docker. health_timeout : attente maximale du
// HEALTHCHECK de l'image à la bascule (durée Go, défaut 2m).
func newDocker(def config.ProviderDef) (*dockerrt.Provider, error) {
	socket := def.Config["socket"]
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	rt := dockerrt.New(def.ID, socket)
	if v := def.Config["health_timeout"]; v != "" {
		d, err := time.ParseDuration(v)
		if err != nil || d <= 0 {
			return nil, fmt.Errorf("health_timeout %q invalide (ex. 90s, 2m)", v)
		}
		rt.HealthTimeout = d
	}
	return rt, nil
}

// newIdentity crée un provider d'identité. keycloak : url est Keycloak vu par
// Symphony, public_url vu par le navigateur (défaut : url) ; l'annuaire
// demande un client de service (directory_client_id, directory_client_secret).
func newIdentity(def config.ProviderDef) (provider.IdentityProvider, error) {
	c := def.Config
	settings := provider.OIDCSettings{
		Issuer:       c["issuer"],
		DiscoveryURL: c["discovery_url"],
		ClientID:     c["client_id"],
		ClientSecret: secretValue(def, "client_secret"),
		RedirectURL:  c["redirect_url"],
		GroupsClaim:  c["groups_claim"],
	}
	switch def.Type {
	case "oidc":
		return oidcidp.New(def.ID, settings)
	case "keycloak":
		return keycloakidp.New(def.ID, settings, keycloakidp.Config{
			URL:                   c["url"],
			PublicURL:             c["public_url"],
			Realm:                 c["realm"],
			DirectoryClientID:     c["directory_client_id"],
			DirectoryClientSecret: secretValue(def, "directory_client_secret"),
		})
	}
	return nil, fmt.Errorf("type de provider identity %q inconnu", def.Type)
}

// newArgoCD crée le runtime Argo CD. Il écrit dans le repo de déploiement avec
// le SCM scm_ref (défaut : SCM par défaut) de la même configuration.
func newArgoCD(def config.ProviderDef, providers *provider.Registry) (*argocdrt.Provider, error) {
	scm, err := providers.SCM(def.Config["scm_ref"])
	if err != nil {
		return nil, err
	}
	token := secretValue(def, "token")
	if token == "" && def.Config["token_env"] == "" {
		token = os.Getenv("ARGOCD_TOKEN")
	}
	c := def.Config
	return argocdrt.New(def.ID, scm, argocdrt.Config{
		URL:           c["url"],
		Token:         token,
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

// secretValue : config[key] (valeur, ou secret déjà résolu), sinon la
// variable d'environnement nommée par config[key_env]
func secretValue(def config.ProviderDef, key string) string {
	if v := def.Config[key]; v != "" {
		return v
	}
	if env := def.Config[key+"_env"]; env != "" {
		return os.Getenv(env)
	}
	return ""
}

// scmToken : jeton du provider (token ou token_env), sinon celui du SCM
// scm_ref de la même configuration, sinon la variable fallbackEnv
func scmToken(def config.ProviderDef, cfg *config.Config, fallbackEnv string) string {
	if token := secretValue(def, "token"); token != "" {
		return token
	}
	if ref := def.Config["scm_ref"]; ref != "" {
		for _, scm := range cfg.Providers.SCM {
			if scm.ID == ref {
				if token := secretValue(scm, "token"); token != "" {
					return token
				}
			}
		}
		if token := os.Getenv(ref + "_TOKEN"); token != "" {
			return token
		}
	}
	if fallbackEnv != "" {
		return os.Getenv(fallbackEnv)
	}
	return ""
}

// gitlabToken : token ou token_env, sinon celui du SCM scm_ref, sinon
// <scm_ref>_TOKEN, sinon GITLAB_TOKEN
func gitlabToken(def config.ProviderDef, cfg *config.Config) string {
	return scmToken(def, cfg, "GITLAB_TOKEN")
}
