package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"time"

	"github.com/clemuscle/symphony/internal/api"
	"github.com/clemuscle/symphony/internal/api/handlers"
	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/notify"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/clemuscle/symphony/internal/webhook"
	officialstacks "github.com/clemuscle/symphony/stacks"
	"github.com/joho/godotenv"
)

func main() {
	// HEALTHCHECK de l'image (scratch : ni curl ni wget) : interroge /healthz
	if len(os.Args) > 1 && os.Args[1] == "-healthcheck" {
		os.Exit(healthcheck())
	}
	// Vérification hors ligne d'une configuration et de stacks
	if len(os.Args) > 1 && os.Args[1] == "validate" {
		os.Exit(runValidate(os.Args[2:], os.Stdout))
	}

	// Logger structuré JSON
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})))

	// Charger .env
	if err := godotenv.Load(); err != nil {
		slog.Info("pas de .env trouvé, utilisation des variables d'environnement")
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

	// Environnements de déploiement (staging, production…), dans l'ordre de promotion
	envs := environments.NewHolder(resolved.Environments)
	if len(resolved.Environments) == 0 {
		slog.Info("pas de section environments : un déploiement par projet")
	}

	// Notifications des équipes (Slack, Teams)
	notifySettings, err := buildNotifications(resolved, providers)
	if err != nil {
		slog.Error("configuration", "error", err)
		os.Exit(1)
	}
	notifier := notify.New(providers, notifySettings)

	// Catalogue : la fiche de chaque projet, lue dans son repo
	store := catalogue.NewStore()
	syncer := gitops.NewSyncer(providers, store, projectRefs(db), catalogueFile(cfg))
	go syncer.Start()

	// Découverte des services existants : les repos des groupes scannés qui
	// ont une fiche deviennent des projets
	discovery, err := buildDiscovery(resolved, providers)
	if err != nil {
		slog.Error("configuration", "error", err)
		os.Exit(1)
	}
	discoverer := gitops.NewDiscoverer(providers, handlers.DiscoveryStore{DB: db, Providers: providers}, syncer.File, func() { go syncer.ForceSync() })
	discoverer.Teams = directoryTeams(providers, security)
	discoverer.SetSettings(discovery)
	go discoverer.Start()

	// Stacks : repo du client, rechargé périodiquement, et catalogue officiel
	stackSrc, stackWarnings, err := stacksSource(cfg)
	if err != nil {
		slog.Error("configuration", "error", err)
		os.Exit(1)
	}
	for _, w := range stackWarnings {
		slog.Warn("configuration à corriger", "detail", w)
	}
	stackLoader := gitops.NewStackLoader(providers, officialstacks.FS(), stackSrc)
	go stackLoader.Start()

	// Tâches asynchrones : reprises au démarrage là où elles s'étaient arrêtées
	runner := tasks.NewRunner(db, providers)
	handlers.RegisterTasks(runner, handlers.TaskDeps{
		Providers: providers,
		DB:        db,
		Stacks:    stackLoader,
		Syncer:    syncer,
		Webhooks:  hooks,
		Store:     store,
		Envs:      envs,
		Notify:    notifier,
	})
	runner.Start(context.Background(), 4)

	// Configuration rechargée à chaud
	watcher := &configWatcher{
		source:    source,
		providers: providers,
		syncer:    syncer,
		discovery: discoverer,
		stacks:    stackLoader,
		security:  security,
		webhooks:  hooks,
		envs:      envs,
		notifier:  notifier,
		codec:     codec,
		interval:  15 * time.Second,
		last:      raw,

		fingerprint: fingerprint(resolved),
		resubscribe: resubscribeWebhooks(db, providers),
	}
	watcher.applied(raw, cfg)
	go watcher.Start()

	// Démarrer le serveur
	addr := ":" + getEnv("PORT", "8080")
	slog.Info("Symphony démarré", "addr", addr)

	if err := http.ListenAndServe(addr, api.NewServer(providers, store, db, syncer, discoverer, stackLoader, security, runner, hooks, envs, notifier, watcher.Status)); err != nil {
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
				Branch:      p.Branch,
				SCMProvider: p.SCMProvider,
			})
		}
		return refs, nil
	}
}

// resubscribeWebhooks réabonne le repo de chaque projet aux webhooks, avec
// l'URL et le secret courants (après une rotation du secret, les forges
// enverraient sinon l'ancien, refusé). Un provider sans webhooks est sauté.
func resubscribeWebhooks(db *database.DB, providers *provider.Registry) func(*webhook.Settings) {
	return func(s *webhook.Settings) {
		projects, err := db.ListProjects()
		if err != nil {
			slog.Error("réabonnement des webhooks : projets illisibles", "error", err)
			return
		}
		done, failed := 0, 0
		for _, p := range projects {
			// Un projet découvert n'a pas de webhook : Symphony n'écrit pas
			// dans les repos qu'il découvre, ses pipelines sont sondées
			if p.RepoPath == "" || p.DiscoveredFrom != "" {
				continue
			}
			scm, err := providers.SCM(p.SCMProvider)
			if err == nil {
				err = scm.EnsureWebhook(p.RepoPath, s.Endpoint(scm.Platform()), s.Secret)
			}
			switch {
			case err == nil:
				done++
			case errors.Is(err, provider.ErrUnsupported):
			default:
				failed++
				slog.Warn("réabonnement du webhook", "project", p.Name, "error", err)
			}
		}
		slog.Info("webhooks réabonnés (nouveau secret ou nouvelle URL)", "projets", done, "échecs", failed)
	}
}
