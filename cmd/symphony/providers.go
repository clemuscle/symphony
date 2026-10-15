package main

import (
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/environments"
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
// registry, enveloppés pour mesurer leurs appels (/metrics). La configuration
// est d'abord validée (types et clés connus, clés requises présentes), puis
// résolue avec les providers de secrets (secret://), qui viennent en premier ;
// elle est renvoyée résolue, pour la politique d'accès et les webhooks. Toute
// erreur fait refuser la configuration entière : une configuration à moitié
// appliquée retirerait des providers qui marchaient.
func buildProviders(cfg *config.Config) (*provider.Registry, *config.Config, error) {
	if err := validateProviders(cfg.Providers); err != nil {
		return nil, nil, err
	}
	providers := provider.NewRegistry()
	if err := registerSecrets(providers, cfg.Providers.Secrets); err != nil {
		return nil, nil, err
	}
	resolved, err := resolveConfig(cfg, providers.Secrets)
	if err != nil {
		return nil, nil, err
	}

	var errs []error
	fail := func(kind string, def config.ProviderDef, err error) {
		errs = append(errs, fmt.Errorf("providers.%s %s : %w", kind, def.ID, err))
	}
	for _, def := range resolved.Providers.SCM {
		p, err := newSCM(def)
		if err != nil {
			fail("scm", def, err)
			continue
		}
		providers.RegisterSCM(instrumented.SCM(p), def.Default)
	}
	for _, def := range resolved.Providers.CI {
		token, err := tokenOf(def, resolved)
		if err != nil {
			fail("ci", def, err)
			continue
		}
		switch def.Type {
		case "gitlab_ci":
			providers.RegisterCI(instrumented.CI(gitlabci.New(def.ID, def.Config["url"], token)), def.Default)
		case "github_actions":
			providers.RegisterCI(instrumented.CI(githubci.New(def.ID, token)), def.Default)
		}
	}
	for _, def := range resolved.Providers.Registry {
		token, err := tokenOf(def, resolved)
		if err != nil {
			fail("registry", def, err)
			continue
		}
		switch def.Type {
		case "gitlab_registry":
			providers.RegisterRegistry(instrumented.Registry(gitlabregistry.New(def.ID, def.Config["url"], def.Config["registry_host"], token)), def.Default)
		case "ghcr":
			providers.RegisterRegistry(instrumented.Registry(ghcr.New(def.ID, def.Config["owner"], token)), def.Default)
		}
	}
	for _, def := range resolved.Providers.Runtime {
		var rt provider.RuntimeProvider
		var err error
		switch def.Type {
		case "docker":
			rt, err = newDocker(def)
		case "argocd":
			rt, err = newArgoCD(def, providers)
		}
		if err != nil {
			fail("runtime", def, err)
			continue
		}
		providers.RegisterRuntime(instrumented.Runtime(rt), def.Default)
	}
	for _, def := range resolved.Providers.Identity {
		p, err := newIdentity(def)
		if err != nil {
			fail("identity", def, err)
			continue
		}
		providers.RegisterIdentity(instrumented.Identity(p), def.Default)
	}
	// Environnements : leurs runtimes doivent exister dans cette configuration
	if err := environments.Validate(resolved.Environments, func(rt string) bool {
		_, err := providers.Runtime(rt)
		return rt != "" && err == nil
	}); err != nil {
		errs = append(errs, err)
	}
	if err := errors.Join(errs...); err != nil {
		return nil, nil, err
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
		return gitlabscm.New(def.ID, def.Config["url"], def.Config["token"]).WithNamespace(def.Config["namespace"]), nil

	case "github":
		return githubscm.New(def.ID, def.Config["token"], def.Config["org"]), nil
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
		ClientSecret: c["client_secret"],
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
			DirectoryClientSecret: c["directory_client_secret"],
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
	c := def.Config
	return argocdrt.New(def.ID, scm, argocdrt.Config{
		URL:           c["url"],
		Token:         c["token"],
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

		SecretStore:         c["secret_store"],
		SecretStoreProvider: orDefault(c["secret_store_provider"], "vault"),
	})
}

// tokenOf : le jeton du provider (token), sinon celui du SCM scm_ref de la
// même configuration (une seule référence au secret pour GitLab et sa CI)
func tokenOf(def config.ProviderDef, cfg *config.Config) (string, error) {
	if token := def.Config["token"]; token != "" {
		return token, nil
	}
	ref := def.Config["scm_ref"]
	for _, scm := range cfg.Providers.SCM {
		if scm.ID == ref {
			return scm.Config["token"], nil
		}
	}
	return "", fmt.Errorf("scm_ref %q : pas de provider SCM de cet id", ref)
}
