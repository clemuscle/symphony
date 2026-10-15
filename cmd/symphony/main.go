package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/clemuscle/symphony/internal/api"
	"github.com/clemuscle/symphony/internal/api/handlers"
	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/clemuscle/symphony/internal/webhook"
	"github.com/joho/godotenv"
)

func main() {
	// HEALTHCHECK de l'image (scratch : ni curl ni wget) : interroge /healthz
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}

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
	providers, resolved, err := buildProviders(cfg)
	if err != nil {
		slog.Error("providers", "error", err)
		os.Exit(1)
	}
	logProviders("providers enregistrés", providers)

	// Authentification et CORS. Le secret de session est commun à toutes les
	// versions de la configuration : un rechargement ne déconnecte personne.
	codec, persistent, err := auth.NewCodec(os.Getenv("SESSION_SECRET"))
	if err != nil {
		slog.Error("SESSION_SECRET", "error", err)
		os.Exit(1)
	}
	if !persistent {
		slog.Warn("SESSION_SECRET absent : secret aléatoire, les sessions ne survivront pas à un redémarrage")
	}
	policy, err := buildPolicy(resolved, providers, codec)
	if err != nil {
		slog.Error("auth", "error", err)
		os.Exit(1)
	}
	security := auth.NewHolder(policy)

	// Webhooks entrants (événements de pipeline)
	hookSettings, err := buildWebhooks(resolved)
	if err != nil {
		slog.Error("webhooks", "error", err)
		os.Exit(1)
	}
	if !hookSettings.Enabled() {
		slog.Info("pas de section webhooks : les tâches suivent les pipelines par sondage")
	}
	hooks := webhook.NewHolder(hookSettings)

	// Catalogue : la fiche de chaque projet, lue dans son repo
	store := catalogue.NewStore()
	syncer := gitops.NewSyncer(providers, store, projectRefs(db), catalogueFile(cfg))
	go syncer.Start()

	// Golden paths : rechargés depuis le repo de templates
	tmplRepo, tmplBranch, tmplDir := templatesSource(cfg)
	tmplLoader := gitops.NewTemplateLoader(providers, tmplRepo, tmplBranch, tmplDir)
	go tmplLoader.Start()

	// Tâches asynchrones : reprises au démarrage là où elles s'étaient arrêtées
	runner := tasks.NewRunner(db, providers)
	handlers.RegisterTasks(runner, handlers.TaskDeps{
		Providers: providers,
		DB:        db,
		Templates: tmplLoader,
		Syncer:    syncer,
		Webhooks:  hooks,
	})
	runner.Start(context.Background(), 4)

	// Configuration rechargée à chaud
	watcher := &configWatcher{
		source:    source,
		providers: providers,
		syncer:    syncer,
		templates: tmplLoader,
		security:  security,
		webhooks:  hooks,
		codec:     codec,
		interval:  15 * time.Second,
		last:      raw,

		fingerprint: fingerprint(resolved),
	}
	go watcher.Start()

	// Démarrer le serveur
	addr := ":" + getEnv("PORT", "8080")
	slog.Info("Symphony démarré", "addr", addr)

	if err := http.ListenAndServe(addr, api.NewServer(providers, store, db, syncer, tmplLoader, security, runner, hooks)); err != nil {
		slog.Error("serveur HTTP", "error", err)
		os.Exit(1)
	}
}

// healthcheck : 0 si le serveur local répond 200 sur /healthz, 1 sinon
func healthcheck() int {
	client := &http.Client{Timeout: 3 * time.Second}
	resp, err := client.Get("http://127.0.0.1:" + getEnv("PORT", "8080") + "/healthz")
	if err != nil {
		return 1
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return 1
	}
	return 0
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

// projectRefs : les projets enregistrés, dont le catalogue lit les fiches.
// Les repos créés par Symphony ont main pour branche par défaut.
func projectRefs(db *database.DB) func() ([]gitops.ProjectRef, error) {
	return func() ([]gitops.ProjectRef, error) {
		projects, err := db.ListProjects()
		if err != nil {
			return nil, err
		}
		refs := make([]gitops.ProjectRef, 0, len(projects))
		for _, p := range projects {
			if p.RepoPath == "" {
				continue
			}
			refs = append(refs, gitops.ProjectRef{
				Name:        p.Name,
				RepoPath:    p.RepoPath,
				Branch:      "main",
				SCMProvider: p.SCMProvider,
			})
		}
		return refs, nil
	}
}
