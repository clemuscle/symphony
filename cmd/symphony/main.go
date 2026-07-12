package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/yourorg/symphony/internal/api"
	"github.com/yourorg/symphony/internal/auth"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/providers"
	gitlabregistry "github.com/yourorg/symphony/internal/providers/artifacts/gitlabregistry"
	gitlabci "github.com/yourorg/symphony/internal/providers/ci/gitlabci"
	dockerdeploy "github.com/yourorg/symphony/internal/providers/deploy/docker"
	gitlabscm "github.com/yourorg/symphony/internal/providers/scm/gitlab"
	"github.com/yourorg/symphony/internal/rbac"
	"github.com/yourorg/symphony/internal/templates"
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
	goldenPathsDir := getEnv("GOLDEN_PATHS_DIR", "config/golden-paths")
	tmplLoader := templates.NewLoader(goldenPathsDir)
	if err := tmplLoader.Load(); err != nil {
		log.Printf("⚠️  templates: %v", err)
	} else {
		log.Printf("✅ %d golden path(s) chargé(s)", len(tmplLoader.GetGoldenPaths()))
	}

	// RBAC — chargé depuis config/rbac.yaml ; permissif si absent (small teams)
	rbacMgr := loadRBAC(getEnv("RBAC_CONFIG_PATH", "config/rbac.yaml"))

	// Auth OIDC — fail-closed par défaut.
	// Sans OIDC_ISSUER, Symphony refuse de démarrer sauf si SYMPHONY_DEV_MODE=1
	// est positionné explicitement (dev local uniquement, jamais en production).
	devMode := os.Getenv("SYMPHONY_DEV_MODE") == "1"
	var authProvider *auth.Provider
	if issuer := os.Getenv("OIDC_ISSUER"); issuer != "" {
		p, err := auth.New(context.Background(), auth.Config{
			Issuer:       issuer,
			ClientID:     os.Getenv("OIDC_CLIENT_ID"),
			ClientSecret: os.Getenv("OIDC_CLIENT_SECRET"),
			RedirectURL:  getEnv("OIDC_REDIRECT_URL", "http://localhost:8090/auth/callback"),
		}, rbacMgr)
		if err != nil {
			log.Fatalf("auth OIDC: %v", err)
		}
		log.Printf("✅ Auth OIDC configurée (issuer: %s)", issuer)
		authProvider = p
	} else if devMode {
		log.Println("⚠️  SYMPHONY_DEV_MODE=1 — auth désactivée (dev local uniquement, jamais en production)")
	} else {
		log.Fatalf("OIDC_ISSUER non configuré. Définir OIDC_ISSUER ou SYMPHONY_DEV_MODE=1 pour le dev local.")
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
			if err := deploy.Ping(); err != nil {
				return "", fmt.Errorf("Docker daemon inaccessible: %w", err)
			}
			return "Docker daemon accessible", nil
		default:
			return "", fmt.Errorf("type inconnu: %s", providerType)
		}
	}

	addr := ":" + getEnv("PORT", "8080")
	srv := api.NewServer(api.ServerOptions{
		Store:        store,
		DB:           db,
		Auth:         authProvider,
		DevMode:      devMode,
		Tmpl:         tmplLoader,
		Providers:    pvds,
		Reload:       initProviders,
		TestProvider: testProvider,
		CfgPath:      cfgPath,
	})
	go reconcileDeployments(db, srv.GetProviders)
	go reconcilePipelines(db, srv.GetProviders)
	log.Printf("🎼 Symphony démarré sur %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv))
}

func buildProviderSet(cfg *providers.IntegrationConfig) (*providers.ProviderSet, error) {
	scm, err := gitlabscm.New(cfg.SCM.URL, cfg.SCM.Token)
	if err != nil {
		return nil, fmt.Errorf("scm: %w", err)
	}
	if err := scm.Ping(); err != nil {
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
	return &providers.ProviderSet{
		SCM:          scm,
		CI:           ci,
		Registry:     registry,
		Deploy:       deploy,
		SCMBaseURL:   cfg.SCM.URL,
		SCMToken:     cfg.SCM.Token,
		CIConfigRepo: cfg.CI.ConfigRepo,
	}, nil
}

func reconcilePipelines(db *database.DB, getProviders func() *providers.ProviderSet) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		pvds := getProviders()
		if pvds == nil {
			continue
		}
		pending, err := db.ListPendingPipelines()
		if err != nil || len(pending) == 0 {
			continue
		}
		for _, p := range pending {
			status, err := pvds.CI.GetPipelineStatus(p.ProjectName, p.PipelineID)
			if err != nil {
				continue
			}
			applied, err := db.UpdatePipelineStatus(p.PipelineID, status)
			if err == nil && applied {
				log.Printf("pipeline %s → %s (project %s)", p.PipelineID, status, p.ProjectName)
			}
		}
	}
}

func reconcileDeployments(db *database.DB, getProviders func() *providers.ProviderSet) {
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()
	for range ticker.C {
		pvds := getProviders()
		if pvds == nil {
			continue
		}
		pending, err := db.ListPendingDeployments()
		if err != nil || len(pending) == 0 {
			continue
		}
		for _, d := range pending {
			project, err := db.GetProject(d.ProjectName)
			if err != nil {
				continue
			}
			status, err := pvds.CI.GetPipelineStatus(project.RepoPath, d.PipelineID)
			if err != nil {
				continue
			}
			var newStatus string
			switch status {
			case "success":
				newStatus = "running"
			case "failed":
				newStatus = "failed"
			case "canceled":
				newStatus = "stopped"
			}
			if newStatus != "" {
				applied, err := db.UpdateDeploymentStatus(d.PipelineID, newStatus)
				if err == nil && applied {
					log.Printf("deployment %s → %s (pipeline %s)", d.ProjectName, newStatus, d.PipelineID)
				}
			}
		}
	}
}

func loadRBAC(path string) *rbac.Manager {
	cfg, err := rbac.LoadConfig(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("⚠️  rbac: %v — rôles par défaut (developer = tous)", err)
		} else {
			log.Println("⚠️  config/rbac.yaml absent — rôles par défaut (developer = tous, admin = personne)")
		}
		return rbac.Default()
	}
	log.Printf("✅ RBAC chargé depuis %s", path)
	return rbac.New(cfg)
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
