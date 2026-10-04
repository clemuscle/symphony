package api

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourorg/symphony/internal/api/handlers"
	"github.com/yourorg/symphony/internal/catalogue"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/provider"
)

type Server struct {
	providers *provider.Registry
	store     *catalogue.Store
	db        *database.DB
	syncer    *gitops.Syncer
	templates *gitops.TemplateLoader
}

func NewServer(
	providers *provider.Registry,
	store *catalogue.Store,
	db *database.DB,
	syncer *gitops.Syncer,
	templates *gitops.TemplateLoader,
) http.Handler {
	s := &Server{
		providers: providers,
		store:     store,
		db:        db,
		syncer:    syncer,
		templates: templates,
	}

	r := chi.NewRouter()
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(requestLogger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	// Health
	r.Get("/healthz", s.healthz)

	// API v1
	r.Route("/api/v1", func(r chi.Router) {
		// Catalogue
		r.Get("/services", handlers.ListServices(store))
		r.Get("/services/{name}", handlers.GetService(store))
		r.Post("/services/{name}/actions/{action}", handlers.TriggerAction(store, providers, db))

		// Golden Paths
		r.Get("/golden-paths", handlers.ListGoldenPaths(templates))

		// Projets
		r.Get("/projects", handlers.ListProjects(db))
		r.Post("/projects", handlers.CreateProject(providers, db, templates, syncer))

		// Repos (liste depuis les SCM providers)
		r.Get("/repos", handlers.ListRepos(providers))

		// Pipelines
		r.Post("/pipelines/trigger", handlers.TriggerPipeline(providers, db))
		r.Get("/pipelines/status", handlers.GetPipelineStatus(providers, db))
		r.Get("/pipelines/*", handlers.ListPipelines(db))

		// Déploiements
		r.Get("/deployments", handlers.ListDeployments(providers, db))
		r.Post("/deployments", handlers.Deploy(providers, db))
		r.Delete("/deployments/{id}", handlers.StopDeployment(providers, db))
		r.Get("/deployments/{id}/logs", handlers.GetLogs(providers))
		r.Post("/deployments/{id}/restart", handlers.RestartDeployment(providers, db))

		// Admin
		r.Get("/providers", handlers.ListProviders(providers))
		r.Post("/sync", handlers.ForceSync(syncer))
		r.Get("/audit", handlers.ListAudit(db))
	})

	slog.Info("API server configuré")
	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}
