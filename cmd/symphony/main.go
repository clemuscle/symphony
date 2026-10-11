package main

import (
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/joho/godotenv"
	"github.com/yourorg/symphony/internal/api"
	"github.com/yourorg/symphony/internal/catalogue"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
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

	// Charger la config : bootstrap local, puis repo Git s'il en désigne un
	source, err := newConfigSource(getEnv("SYMPHONY_CONFIG", "symphony.yaml"))
	if err != nil {
		slog.Error("chargement config", "error", err)
		os.Exit(1)
	}
	cfg, raw, err := source.load()
	if err != nil {
		slog.Error("chargement config", "error", err)
		os.Exit(1)
	}
	slog.Info("configuration chargée", source.desc...)

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

	// Providers de la config
	providers, err := buildProviders(cfg)
	if err != nil {
		slog.Error("providers", "error", err)
		os.Exit(1)
	}
	logProviders("providers enregistrés", providers)

	// Catalogue + GitOps syncer
	store := catalogue.NewStore()
	syncer := gitops.NewSyncer(
		providers,
		store,
		cfg.Catalogue.Repo,
		getEnv("CATALOGUE_BRANCH", "main"),
		cfg.Catalogue.Path,
	)
	go syncer.Start()

	// Golden paths : rechargés depuis le repo de templates
	tmplRepo, tmplBranch, tmplDir := templatesSource(cfg)
	tmplLoader := gitops.NewTemplateLoader(providers, tmplRepo, tmplBranch, tmplDir)
	go tmplLoader.Start()

	// Configuration rechargée à chaud
	watcher := &configWatcher{
		source:    source,
		providers: providers,
		syncer:    syncer,
		templates: tmplLoader,
		interval:  15 * time.Second,
		last:      raw,
	}
	go watcher.Start()

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

func orDefault(v, fallback string) string {
	if v != "" {
		return v
	}
	return fallback
}
