package api

import (
	"log/slog"
	"net/http"

	"github.com/clemuscle/symphony/internal/api/handlers"
	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/web"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
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
	security *auth.Holder,
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
	r.Use(metrics.Middleware) // avant Recoverer : une panique rattrapée compte comme un 500
	r.Use(middleware.Recoverer)
	r.Use(security.CORS)

	// Health et métriques Prometheus (publics : à protéger au niveau réseau)
	r.Get("/healthz", s.healthz)
	r.Handle("/metrics", metrics.Handler())

	// Connexion OIDC (Backend-for-Frontend : le navigateur ne voit pas de jeton)
	r.Get("/auth/login", security.Login)
	r.Get("/auth/callback", security.Callback)
	r.Post("/auth/logout", security.Logout)

	// API v1 : utilisateur reconnu, puis rôle minimum par route
	viewer, developer, admin := auth.Require(auth.RoleViewer), auth.Require(auth.RoleDeveloper), auth.Require(auth.RoleAdmin)
	r.Route("/api/v1", func(r chi.Router) {
		r.Use(security.Authenticate)
		r.Get("/me", security.Me) // sans rôle : le front affiche « aucun rôle »

		// Catalogue
		r.With(viewer).Get("/services", handlers.ListServices(store))
		r.With(viewer).Get("/services/{name}", handlers.GetService(store))
		r.With(developer).Post("/services/{name}/actions/{action}", handlers.TriggerAction(store, providers, db))

		// Golden Paths
		r.With(viewer).Get("/golden-paths", handlers.ListGoldenPaths(templates))

		// Projets
		r.With(viewer).Get("/projects", handlers.ListProjects(db))
		r.With(developer).Post("/projects", handlers.CreateProject(providers, db, templates, syncer))
		r.With(admin).Delete("/projects/{name}", handlers.DeleteProject(providers, db, syncer))
		r.With(viewer).Get("/projects/{name}/image", handlers.GetProjectImage(providers, db))

		// Repos (liste depuis les SCM providers)
		r.With(viewer).Get("/repos", handlers.ListRepos(providers))

		// Pipelines
		r.With(developer).Post("/pipelines/trigger", handlers.TriggerPipeline(providers, db))
		r.With(viewer).Get("/pipelines/status", handlers.GetPipelineStatus(providers, db))
		r.With(viewer).Get("/pipelines/*", handlers.ListPipelines(db))

		// Déploiements (les logs peuvent contenir des données sensibles : developer)
		r.With(viewer).Get("/deployments", handlers.ListDeployments(providers, db))
		r.With(developer).Post("/deployments", handlers.Deploy(providers, db))
		r.With(developer).Delete("/deployments/{id}", handlers.StopDeployment(providers, db))
		r.With(developer).Get("/deployments/{id}/logs", handlers.GetLogs(providers))
		r.With(developer).Post("/deployments/{id}/restart", handlers.RestartDeployment(providers, db))

		// Admin
		r.With(viewer).Get("/providers", handlers.ListProviders(providers))
		r.With(admin).Post("/sync", handlers.ForceSync(syncer, templates, db))
		r.With(admin).Get("/audit", handlers.ListAudit(db))
	})

	// Front Vue embarqué : tout ce qui n'est pas une route de l'API
	r.Handle("/*", web.Handler())

	slog.Info("API server configuré")
	return r
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{
		"status": "ok",
	})
}
