package api

import (
	"net/http"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourorg/symphony/internal/auth"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/providers"
	"github.com/yourorg/symphony/internal/templates"
)

type Server struct {
	store   *catalog.Store
	db      *database.DB
	auth    *auth.Provider
	tmpl    *templates.Loader
	cfgPath string
	router  *chi.Mux
	// providers protégés par mutex — nil = setup requis
	mu     sync.RWMutex
	pvds   *providers.ProviderSet
	reload func() (*providers.ProviderSet, error) // callback défini dans main.go
	// callback de test de connexion (évite d'importer les drivers ici)
	testProvider func(providerType string, cfg map[string]string) (string, error)
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.router.ServeHTTP(w, r)
}

// GetProviders est thread-safe et reflète toujours le ProviderSet courant,
// y compris après reconfiguration via le wizard ou /config/reload.
func (s *Server) GetProviders() *providers.ProviderSet {
	return s.getProviders()
}

func (s *Server) getProviders() *providers.ProviderSet {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.pvds
}

func (s *Server) setProviders(ps *providers.ProviderSet) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pvds = ps
}

type ServerOptions struct {
	Store        *catalog.Store
	DB           *database.DB
	Auth         *auth.Provider
	Tmpl         *templates.Loader
	Providers    *providers.ProviderSet // nil si pas encore configuré
	Reload       func() (*providers.ProviderSet, error)
	TestProvider func(providerType string, cfg map[string]string) (string, error)
	CfgPath      string
}

func NewServer(opts ServerOptions) *Server {
	s := &Server{
		store:        opts.Store,
		db:           opts.DB,
		auth:         opts.Auth,
		tmpl:         opts.Tmpl,
		pvds:         opts.Providers,
		reload:       opts.Reload,
		testProvider: opts.TestProvider,
		cfgPath:      opts.CfgPath,
	}
	r := chi.NewRouter()
	s.router = r
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/healthz", s.healthz)

	if s.auth != nil {
		r.Get("/auth/login", s.auth.LoginHandler)
		r.Get("/auth/callback", s.auth.CallbackHandler)
		r.Get("/auth/logout", s.auth.LogoutHandler)
	}

	r.Group(func(r chi.Router) {
		if s.auth != nil {
			r.Use(s.auth.Middleware)
		}

		r.Get("/api/v1/auth/me", s.me)

		// Setup wizard — accessible à tous les utilisateurs authentifiés pour /status,
		// admin uniquement pour /test, /save, /reload
		r.Get("/api/v1/setup/status", s.setupStatus)
		r.With(s.adminOnly).Post("/api/v1/setup/test", s.setupTest)
		r.With(s.adminOnly).Post("/api/v1/setup/save", s.setupSave)
		r.With(s.adminOnly).Post("/api/v1/config/reload", s.reloadConfig)

		// Catalogue
		r.Get("/api/v1/services", s.listServices)
		r.Get("/api/v1/services/{name}", s.getService)
		r.Post("/api/v1/services/{name}/actions/{action}", s.triggerAction)

		// Golden Paths
		r.Get("/api/v1/golden-paths", s.listGoldenPaths)
		r.Post("/api/v1/templates/reload", s.reloadTemplates)

		// Projets
		r.With(s.deployerOnly).Post("/api/v1/projects", s.createProject)
		r.Get("/api/v1/projects", s.listProjects)
		r.Get("/api/v1/projects/{name}/steps", s.listProjectSteps)
		r.Get("/api/v1/repos", s.listRepos)

		// Recettes
		r.Get("/api/v1/projects/{name}/recettes", s.listRecettes)
		r.With(s.deployerOnly).Post("/api/v1/projects/{name}/recettes", s.createRecette)
		r.With(s.deployerOnly).Delete("/api/v1/projects/{name}/recettes/{recette}", s.destroyRecette)

		// Pipelines
		r.With(s.deployerOnly).Post("/api/v1/pipelines/trigger", s.triggerPipelineHandler)
		r.Get("/api/v1/pipelines/status", s.getPipelineStatusHandler)
		r.Get("/api/v1/pipelines/{project}", s.listPipelinesHandler)

		// Déploiements
		r.Get("/api/v1/deployments", s.listDeployments)
		r.With(s.deployerOnly).Post("/api/v1/deployments", s.deployProject)
		r.With(s.deployerOnly).Delete("/api/v1/deployments/{id}", s.stopDeployment)

		// Audit
		r.Get("/api/v1/audit", s.listAudit)
	})

	return s
}

func (s *Server) adminOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		user, ok := auth.UserFromContext(r.Context())
		if !ok || !user.IsAdmin {
			respond(w, http.StatusForbidden, map[string]string{"error": "admin required"})
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) deployerOnly(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.auth == nil {
			next.ServeHTTP(w, r) // dev mode: no OIDC configured
			return
		}
		user, ok := auth.UserFromContext(r.Context())
		if !ok || !s.auth.CanDeploy(user) {
			respond(w, http.StatusForbidden, map[string]string{"error": "droits insuffisants"})
			return
		}
		next.ServeHTTP(w, r)
	})
}
