package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/joho/godotenv"
	"github.com/yourorg/symphony/internal/api"
	"github.com/yourorg/symphony/internal/auth"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/costs"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/drivers"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/providers"
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

	// Catalogue + GitOps sync — syncer hissé ici pour pouvoir être retargeté
	// à chaud (UpdateConfig) depuis initProviders sans redémarrer le process.
	// syncerMu sérialise le check-then-act sur syncer : initProviders est
	// appelable concurremment depuis le wizard et /config/reload, sans elle
	// deux reloads simultanés pourraient tous deux le trouver nil et lancer
	// deux goroutines Start(), ou se marcher dessus sur le pointeur lui-même.
	store := catalog.NewStore()
	var syncer *gitops.Syncer
	var syncerMu sync.Mutex

	startOrUpdateSyncer := func(c *providers.IntegrationConfig) {
		syncerMu.Lock()
		defer syncerMu.Unlock()
		if syncer == nil {
			syncer = gitops.NewSyncer(c.SCM.URL, c.SCM.Token, c.CI.ConfigRepo, store)
			go syncer.Start()
		} else {
			syncer.UpdateConfig(c.SCM.URL, c.SCM.Token, c.CI.ConfigRepo)
		}
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
		pvds, err := drivers.BuildProviderSet(c)
		if err != nil {
			return nil, err
		}
		startOrUpdateSyncer(c)
		return pvds, nil
	}

	if cfg.IsConfigured() {
		pvds, err = drivers.BuildProviderSet(cfg)
		if err != nil {
			log.Printf("⚠️  providers: %v — démarrage en mode setup", err)
			pvds = nil
		} else {
			log.Println("✅ Providers initialisés")
			startOrUpdateSyncer(cfg)
		}
	} else {
		log.Println("⚠️  Providers non configurés — démarrez le wizard d'initialisation")
	}

	// Template loader
	goldenPathsDir := getEnv("GOLDEN_PATHS_DIR", "config/golden-paths")
	tmplLoader := templates.NewLoader(goldenPathsDir)
	if err := tmplLoader.Load(); err != nil {
		log.Printf("⚠️  templates: %v", err)
	} else {
		log.Printf("✅ %d golden path(s) chargé(s)", len(tmplLoader.GetGoldenPaths()))
	}

	// Coûts — chargé depuis config/costs.yaml ; tarifs à zéro si absent (tracking usage seulement)
	costCfg := loadCosts(getEnv("COSTS_CONFIG_PATH", "config/costs.yaml"))

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

	if os.Getenv("GITLAB_WEBHOOK_SECRET") == "" {
		log.Println("⚠️  GITLAB_WEBHOOK_SECRET absent — webhook GitLab désactivé (fail-closed), la réconciliation 30s reste active")
	}

	addr := ":" + getEnv("PORT", "8080")
	srv := api.NewServer(api.ServerOptions{
		Store:          store,
		DB:             db,
		Auth:           authProvider,
		DevMode:        devMode,
		Tmpl:           tmplLoader,
		Providers:      pvds,
		Reload:         initProviders,
		TestProvider:   drivers.TestProvider,
		AvailableTypes: drivers.AvailableTypes(),
		CfgPath:        cfgPath,
		CostCfg:        costCfg,
	})
	go reconcileDeployments(db, srv.GetProviders)
	go reconcilePipelines(db, srv.GetProviders)
	log.Printf("🎼 Symphony démarré sur %s", addr)
	log.Fatal(http.ListenAndServe(addr, srv))
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

		// Pass 1 — pending : attend le résultat du pipeline CI.
		if pending, err := db.ListPendingDeployments(); err == nil {
			for _, d := range pending {
				project, err := db.GetProject(d.ProjectName)
				if err != nil {
					continue
				}
				status, err := pvds.CI.GetPipelineStatus(project.RepoPath, d.PipelineID)
				if err != nil {
					continue
				}
				var next string
				switch status {
				case "success":
					next = "running"
				case "failed":
					next = "failed"
				case "canceled":
					next = "stopped"
				}
				if next != "" {
					if applied, err := db.UpdateDeploymentStatus(d.PipelineID, next); err == nil && applied {
						log.Printf("deployment %s → %s (pipeline %s)", d.ProjectName, next, d.PipelineID)
					}
				}
			}
		}

		// Pass 2 — running : détecte les containers disparus hors de Symphony.
		live, err := pvds.Deploy.ListContainers()
		if err != nil {
			log.Printf("reconcile: list containers: %v", err)
			continue
		}
		liveNames := make(map[string]bool, len(live))
		for _, c := range live {
			liveNames[c.Name] = true
		}

		if running, err := db.ListRunningDeployments(); err == nil {
			for _, d := range running {
				if !liveNames[d.ProjectName] {
					if applied, err := db.MarkContainerStopped(d.ProjectName); err == nil && applied {
						log.Printf("deployment %s → stopped (container disparu)", d.ProjectName)
					}
				}
			}
		}

		if recettes, err := db.ListRunningRecettes(); err == nil {
			for _, r := range recettes {
				if !liveNames[r.RecetteName] {
					if applied, err := db.MarkContainerStopped(r.RecetteName); err == nil && applied {
						log.Printf("recette %s → stopped (container disparu)", r.RecetteName)
					}
				}
			}
		}
	}
}

func loadCosts(path string) costs.Config {
	cfg, err := costs.LoadConfig(path)
	if err != nil {
		if !os.IsNotExist(err) {
			log.Printf("⚠️  costs: %v — tarifs à zéro (usage tracké, pas de valorisation)", err)
		}
		return costs.DefaultConfig()
	}
	log.Printf("✅ Coûts chargés depuis %s (container: %.4f %s/h, CI: %.4f %s/min)",
		path, cfg.Rates.ContainerHourly, cfg.Currency, cfg.Rates.CIMinute, cfg.Currency)
	return cfg
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
