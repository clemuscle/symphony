package api

import (
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/providers"
	"github.com/yourorg/symphony/internal/templates"
)

type Server struct {
	store    *catalog.Store
	db       *database.DB
	scm      providers.SCMProvider
	ci       providers.CIProvider
	registry providers.RegistryProvider
	deploy   providers.DeployProvider
	tmpl     *templates.Loader
}

func NewServer(
	store *catalog.Store,
	db *database.DB,
	scm providers.SCMProvider,
	ci providers.CIProvider,
	registry providers.RegistryProvider,
	deploy providers.DeployProvider,
	tmpl *templates.Loader,
) *chi.Mux {
	s := &Server{store: store, db: db, scm: scm, ci: ci, registry: registry, deploy: deploy, tmpl: tmpl}
	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/healthz", s.healthz)

	// Catalogue
	r.Get("/api/v1/services", s.listServices)
	r.Get("/api/v1/services/{name}", s.getService)
	r.Post("/api/v1/services/{name}/actions/{action}", s.triggerAction)

	// Golden Paths
	r.Get("/api/v1/golden-paths", s.listGoldenPaths)
	r.Post("/api/v1/templates/reload", s.reloadTemplates)

	// Projets
	r.Post("/api/v1/projects", s.createProject)
	r.Get("/api/v1/projects", s.listProjects)
	r.Get("/api/v1/repos", s.listRepos)

	// Pipelines
	r.Post("/api/v1/pipelines/trigger", s.triggerPipelineHandler)
	r.Get("/api/v1/pipelines/status", s.getPipelineStatusHandler)
	r.Get("/api/v1/pipelines/{project}", s.listPipelinesHandler)

	// Déploiements
	r.Get("/api/v1/deployments", s.listDeployments)
	r.Post("/api/v1/deployments", s.deployProject)
	r.Delete("/api/v1/deployments/{id}", s.stopDeployment)

	// Audit
	r.Get("/api/v1/audit", s.listAudit)

	return r
}
