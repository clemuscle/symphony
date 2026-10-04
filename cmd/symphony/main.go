package main

import (
	"log/slog"
	"net/http"
	"os"

	"github.com/joho/godotenv"
	"github.com/yourorg/symphony/internal/api"
	"github.com/yourorg/symphony/internal/catalogue"
	"github.com/yourorg/symphony/internal/config"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/provider"
	gitlabscm "github.com/yourorg/symphony/internal/provider/scm/gitlab"
	githubscm "github.com/yourorg/symphony/internal/provider/scm/github"
	gitlabci "github.com/yourorg/symphony/internal/provider/ci/gitlabci"
	githubci "github.com/yourorg/symphony/internal/provider/ci/githubactions"
	dockerrt "github.com/yourorg/symphony/internal/provider/runtime/docker"
	k8srt "github.com/yourorg/symphony/internal/provider/runtime/kubernetes"
)

func main() {
	// Logger structuré JSON
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Charger .env
	if err := godotenv.Load(); err != nil {
		slog.Warn("pas de .env trouvé, utilisation des variables d'environnement")
	}

	// Charger la config
	cfg, err := config.Load(getEnv("SYMPHONY_CONFIG", "symphony.yaml"))
	if err != nil {
		slog.Error("chargement config", "error", err)
		os.Exit(1)
	}

	// Base de données
	db, err := database.Connect()
	if err != nil {
		slog.Error("connexion DB", "error", err)
		os.Exit(1)
	}
	if err := db.Migrate(); err != nil {
		slog.Error("migration DB", "error", err)
		os.Exit(1)
	}

	// Provider registry
	providers := provider.NewRegistry()

	// Enregistrer les providers depuis la config
	for _, def := range cfg.Providers.SCM {
		switch def.Type {
		case "gitlab":
			url := def.Config["url"]
			token := os.Getenv(def.Config["token_env"])
			if token == "" {
				token = def.Config["token"]
			}
			p := gitlabscm.New(def.ID, url, token)
			providers.RegisterSCM(p, def.Default)
			slog.Info("provider SCM enregistré", "id", def.ID, "type", def.Type)

		case "github":
			token := os.Getenv(def.Config["token_env"])
			org := def.Config["org"]
			p := githubscm.New(def.ID, token, org)
			providers.RegisterSCM(p, def.Default)
			slog.Info("provider SCM enregistré", "id", def.ID, "type", def.Type)
		}
	}

	for _, def := range cfg.Providers.CI {
		switch def.Type {
		case "gitlab_ci":
			url := def.Config["url"]
			token := os.Getenv(def.Config["token_env"])
			if token == "" {
				// Réutiliser le token du SCM GitLab référencé
				scmRef := def.Config["scm_ref"]
				token = os.Getenv(scmRef + "_TOKEN")
				if token == "" {
					token = os.Getenv("GITLAB_TOKEN")
				}
			}
			p := gitlabci.New(def.ID, url, token)
			providers.RegisterCI(p, def.Default)
			slog.Info("provider CI enregistré", "id", def.ID, "type", def.Type)

		case "github_actions":
			token := os.Getenv(def.Config["token_env"])
			p := githubci.New(def.ID, token)
			providers.RegisterCI(p, def.Default)
			slog.Info("provider CI enregistré", "id", def.ID, "type", def.Type)
		}
	}

	for _, def := range cfg.Providers.Runtime {
		switch def.Type {
		case "docker":
			socket := def.Config["socket"]
			if socket == "" {
				socket = "/var/run/docker.sock"
			}
			p := dockerrt.New(def.ID, socket)
			providers.RegisterRuntime(p, def.Default)
			slog.Info("provider runtime enregistré", "id", def.ID, "type", def.Type)

		case "kubernetes":
			p := k8srt.New(def.ID)
			providers.RegisterRuntime(p, def.Default)
			slog.Info("provider runtime enregistré (stub)", "id", def.ID, "type", def.Type)
		}
	}

	// Catalogue + GitOps syncer
	store := catalogue.NewStore()
	defaultSCM, err := providers.SCM("")
	if err != nil {
		slog.Error("pas de provider SCM par défaut", "error", err)
		os.Exit(1)
	}

	syncer := gitops.NewSyncer(
		defaultSCM,
		store,
		cfg.Catalogue.Repo,
		getEnv("CATALOGUE_BRANCH", "main"),
		cfg.Catalogue.Path,
	)
	go syncer.Start()

	// Template loader
	templateRepo := getEnv("TEMPLATES_REPO", cfg.Catalogue.Repo)
	tmplLoader := gitops.NewTemplateLoader(defaultSCM, templateRepo, "main")
	tmplLoader.Load(cfg.GoldenPaths)

	// Démarrer le serveur
	addr := ":" + getEnv("PORT", "8080")
	slog.Info("Symphony démarré", "addr", addr)

	if err := http.ListenAndServe(addr, api.NewServer(providers, store, db, syncer, tmplLoader)); err != nil {
		slog.Error("serveur HTTP", "error", err)
		os.Exit(1)
	}
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
