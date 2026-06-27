package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/yourorg/symphony/internal/api"
	"github.com/yourorg/symphony/internal/auth"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/providers"
	"github.com/yourorg/symphony/internal/templates"
	gitlabregistry "github.com/yourorg/symphony/internal/providers/artifacts/gitlabregistry"
	gitlabci "github.com/yourorg/symphony/internal/providers/ci/gitlabci"
	dockerdeploy "github.com/yourorg/symphony/internal/providers/deploy/docker"
	gitlabscm "github.com/yourorg/symphony/internal/providers/scm/gitlab"
)

func main() {
	godotenv.Load()

	// Base de données
	db, err := database.Connect()
	if err != nil {
		log.Fatalf("DB: %v", err)
	}
	if err := db.Migrate(); err != nil {
		log.Fatalf("migration: %v", err)
	}
	log.Println("✅ PostgreSQL connecté")

	// Config providers — YAML + override env vars
	cfgPath := getEnv("CONFIG_PATH", providers.DefaultConfigPath)
	var pvds *providers.ProviderSet

	cfg, err := providers.LoadConfig(cfgPath)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("⚠️  config providers: %v", err)
		}
		// Pas de fichier config — on démarre en mode setup
		cfg = &providers.IntegrationConfig{}
		cfg.ApplyEnvOverrides()
	}

	// Callback de réinitialisation des providers (utilisé par le wizard et /config/reload)
	initProviders := func() (*providers.ProviderSet, error) {
		c, err := providers.LoadConfig(cfgPath)
		if err != nil {
			// Si le fichier n'existe pas encore (juste après le wizard qui vient de l'écrire),
			// on relit directement depuis les env vars
			c = &providers.IntegrationConfig{}
			c.ApplyEnvOverrides()
		}
		return buildProviderSet(c)
	}

	if cfg.IsConfigured() {
		pvds, err = buildProviderSet(cfg)
		if err != nil {
			log.Printf("⚠️  providers: %v — démarrage en mode setup", err)
			pvds = nil
		} else {
			log.Println("✅ Providers initialisés")
			// Runner CI — best-effort
			runnerName := getEnv("RUNNER_NAME", "symphony-runner")
			runnerExecutor := getEnv("RUNNER_EXECUTOR_TYPE", "docker")
			if err := pvds.CI.EnsureRunner(runnerName, runnerExecutor); err != nil {
				log.Printf("⚠️  EnsureRunner: %v — les pipelines déclenchés échoueront jusqu'à résolution manuelle", err)
			} else {
				log.Println("✅ runner CI disponible")
			}
		}
	} else {
		log.Println("⚠️  Providers non configurés — démarrez le wizard d'initialisation")
	}

	// Catalogue + GitOps sync
	store := catalog.NewStore()
	if cfg.IsConfigured() {
		syncer := gitops.NewSyncer(cfg.SCM.URL, cfg.SCM.Token, cfg.CI.ConfigRepo, store)
		go syncer.Start()
	}

	// Template loader
	var tmplLoader *templates.Loader
	if cfg.IsConfigured() {
		tmplLoader = templates.NewLoader(cfg.SCM.URL, cfg.SCM.Token, cfg.CI.TemplatesRepo)
		if err := tmplLoader.Load(); err != nil {
			log.Printf("⚠️  templates: %v", err)
		} else {
			log.Printf("✅ %d golden path(s) chargé(s)", len(tmplLoader.GetGoldenPaths()))
		}
	} else {
		tmplLoader = templates.NewLoader("", "", "")
	}

	// Auth OIDC
	var authProvider *auth.Provider
	if issuer := os.Getenv("OIDC_ISSUER"); issuer != "" {
		p, err := auth.New(context.Background(), auth.Config{
			Issuer:       issuer,
			ClientID:     os.Getenv("OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
			RedirectURL:  getEnv("OIDC_REDIRECT_URL", "http://localhost:8090/auth/callback"),
		})
		if err != nil {
			log.Fatalf("auth OIDC: %v", err)
		}
		log.Printf("✅ Auth OIDC configurée (issuer: %s)", issuer)
		authProvider = p
	} else {
		log.Println("⚠️  OIDC_ISSUER absent — auth désactivée (dev uniquement)")
	}

	// Test de connexion provider (utilisé par le wizard — connaît tous les drivers)
	testProvider := func(providerType string, config map[string]string) (string, error) {
		switch providerType {
		case "gitlab":
			scm, err := gitlabscm.New(config["url"], config["token"])
			if err != nil {
				return "", err
			}
			repos, err := scm.ListRepos()
			if err != nil {
				return "", fmt.Errorf("GitLab inaccessible: %w", err)
			}
			return fmt.Sprintf("GitLab accessible — %d dépôts visibles", len(repos)), nil
		case "docker":
			socket := config["socket"]
			if socket == "" {
				socket = "/var/run/docker.sock"
			}
			deploy, err := dockerdeploy.New(socket)
			if err != nil {
				return "", err
			}
			if _, err := deploy.List(); err != nil {
				return "", fmt.Errorf("Docker daemon inaccessible: %w", err)
			}
			return "Docker daemon accessible", nil
		default:
			return "", fmt.Errorf("type inconnu: %s", providerType)
		}
	}

	addr := ":" + getEnv("PORT", "8080")
	log.Printf("🎼 Symphony démarré sur %s", addr)
	log.Fatal(http.ListenAndServe(addr, api.NewServer(api.ServerOptions{
		Store:        store,
		DB:           db,
		Auth:         authProvider,
		Tmpl:         tmplLoader,
		Providers:    pvds,
		Reload:       initProviders,
		TestProvider: testProvider,
		CfgPath:      cfgPath,
	})))
}

func buildProviderSet(cfg *providers.IntegrationConfig) (*providers.ProviderSet, error) {
	scm, err := gitlabscm.New(cfg.SCM.URL, cfg.SCM.Token)
	if err != nil {
		return nil, fmt.Errorf("scm: %w", err)
	}
	ci, err := gitlabci.New(cfg.SCM.URL, cfg.SCM.Token, cfg.CI.ConfigRepo)
	if err != nil {
		return nil, fmt.Errorf("ci: %w", err)
	}
	registryURL := cfg.Registry.URL
	if registryURL == "" {
		registryURL = cfg.SCM.URL
	}
	registry, err := gitlabregistry.New(cfg.SCM.URL, registryURL, cfg.SCM.Token)
	if err != nil {
		return nil, fmt.Errorf("registry: %w", err)
	}
	socket := cfg.Deploy.Socket
	if socket == "" {
		socket = "/var/run/docker.sock"
	}
	deploy, err := dockerdeploy.New(socket)
	if err != nil {
		return nil, fmt.Errorf("deploy: %w", err)
	}
	return &providers.ProviderSet{SCM: scm, CI: ci, Registry: registry, Deploy: deploy}, nil
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
