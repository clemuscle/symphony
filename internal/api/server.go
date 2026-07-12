package api

import (
	"io/fs"
	"net/http"
	"strings"
	"sync"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/yourorg/symphony/internal/auth"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/costs"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/providers"
	"github.com/yourorg/symphony/internal/rbac"
	"github.com/yourorg/symphony/internal/templates"
	"github.com/yourorg/symphony/internal/web"
)

type Server struct {
	store   *catalog.Store
	db      *database.DB
	auth    *auth.Provider
	devMode bool // true = SYMPHONY_DEV_MODE=1, jamais en production
	tmpl    *templates.Loader
	cfgPath string
	costCfg costs.Config
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
	DevMode      bool // SYMPHONY_DEV_MODE=1 — désactive l'auth pour le dev local
	Tmpl         *templates.Loader
	Providers    *providers.ProviderSet // nil si pas encore configuré
	Reload       func() (*providers.ProviderSet, error)
	TestProvider func(providerType string, cfg map[string]string) (string, error)
	CfgPath      string
	CostCfg      costs.Config
}

func NewServer(opts ServerOptions) *Server {
	s := &Server{
		store:        opts.Store,
		db:           opts.DB,
		auth:         opts.Auth,
		devMode:      opts.DevMode,
		tmpl:         opts.Tmpl,
		pvds:         opts.Providers,
		reload:       opts.Reload,
		testProvider: opts.TestProvider,
		cfgPath:      opts.CfgPath,
		costCfg:      opts.CostCfg,
	}
	r := chi.NewRouter()
	s.router = r
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/healthz", s.healthz)

	// Frontend embarqué — SPA fallback : toute route non-API sert index.html
	staticFS, _ := fs.Sub(web.Static, "static")
	fileServer := http.FileServer(http.FS(staticFS))
	r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/auth/") {
			http.NotFound(w, r)
			return
		}
		// Tester si le fichier existe dans le FS embarqué
		path := strings.TrimPrefix(r.URL.Path, "/")
		if path == "" {
			path = "index.html"
		}
		f, err := staticFS.Open(path)
		if err != nil {
			// Fichier absent → SPA fallback sur index.html
			r.URL.Path = "/"
		} else {
			f.Close()
		}
		fileServer.ServeHTTP(w, r)
	})

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

		// Setup wizard — /status accessible à tous ; /test, /save, /reload réservés admin
		r.Get("/api/v1/setup/status", s.setupStatus)
		r.With(s.requireRole(rbac.RoleAdmin)).Post("/api/v1/setup/test", s.setupTest)
		r.With(s.requireRole(rbac.RoleAdmin)).Post("/api/v1/setup/save", s.setupSave)
		r.With(s.requireRole(rbac.RoleAdmin)).Post("/api/v1/config/reload", s.reloadConfig)

		// Catalogue (lecture = viewer+)
		r.Get("/api/v1/services", s.listServices)
		r.Get("/api/v1/services/{name}", s.getService)
		r.With(s.requireRole(rbac.RoleDeveloper)).Post("/api/v1/services/{name}/actions/{action}", s.triggerAction)

		// Golden Paths (lecture = viewer+, rechargement = admin)
		r.Get("/api/v1/golden-paths", s.listGoldenPaths)
		r.With(s.requireRole(rbac.RoleAdmin)).Post("/api/v1/templates/reload", s.reloadTemplates)

		// Projets (lecture = viewer+, création = developer+)
		r.With(s.requireRole(rbac.RoleDeveloper)).Post("/api/v1/projects", s.createProject)
		r.Get("/api/v1/projects", s.listProjects)
		r.Get("/api/v1/projects/{name}/steps", s.listProjectSteps)
		r.Get("/api/v1/repos", s.listRepos)
		r.Get("/api/v1/namespaces", s.listNamespaces)

		// Recettes (lecture = viewer+, création/destruction = developer+)
		r.Get("/api/v1/projects/{name}/recettes", s.listRecettes)
		r.With(s.requireRole(rbac.RoleDeveloper)).Post("/api/v1/projects/{name}/recettes", s.createRecette)
		r.With(s.requireRole(rbac.RoleDeveloper)).Delete("/api/v1/projects/{name}/recettes/{recette}", s.destroyRecette)

		// Pipelines (lecture = viewer+, déclenchement = developer+)
		r.With(s.requireRole(rbac.RoleDeveloper)).Post("/api/v1/pipelines/trigger", s.triggerPipelineHandler)
		r.Get("/api/v1/pipelines/status", s.getPipelineStatusHandler)
		r.Get("/api/v1/pipelines/{project}", s.listPipelinesHandler)

		// Déploiements (lecture = viewer+, création/arrêt = developer+)
		r.Get("/api/v1/deployments", s.listDeployments)
		r.With(s.requireRole(rbac.RoleDeveloper)).Post("/api/v1/deployments", s.deployProject)
		r.With(s.requireRole(rbac.RoleDeveloper)).Delete("/api/v1/deployments/{id}", s.stopDeployment)

		// Inventaire des ressources actives (lecture = viewer+)
		r.Get("/api/v1/inventory", s.getInventory)

		// Coûts par projet / équipe (lecture = viewer+)
		r.Get("/api/v1/costs", s.getCosts)

		// Audit (lecture = viewer+)
		r.Get("/api/v1/audit", s.listAudit)
	})

	return s
}

func (s *Server) requireRole(min rbac.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if s.devMode {
				next.ServeHTTP(w, r)
				return
			}
			user, ok := auth.UserFromContext(r.Context())
			if !ok || !user.Role.AtLeast(min) {
				respond(w, http.StatusForbidden, map[string]string{
					"error":    "forbidden",
					"required": string(min),
				})
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}
