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
	store    *catalog.Store
	db       *database.DB
	auth     *auth.Provider
	devMode  bool // true = SYMPHONY_DEV_MODE=1, jamais en production
	tmpl     *templates.Loader
	cfgPath  string
	rbac     *rbac.Store
	rbacPath string
	costCfg  costs.Config
	router   *chi.Mux
	// providers protégés par mutex — nil = setup requis
	mu     sync.RWMutex
	pvds   *providers.ProviderSet
	reload func() (*providers.ProviderSet, error) // callback défini dans main.go
	// callback de test de connexion (évite d'importer les drivers ici)
	testProvider func(category, providerType string, cfg map[string]string) (string, error)
	// types de driver compilés par catégorie, pour peupler les sélecteurs du wizard
	availableTypes map[string][]string
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
	Store          *catalog.Store
	DB             *database.DB
	Auth           *auth.Provider
	DevMode        bool // SYMPHONY_DEV_MODE=1 — désactive l'auth pour le dev local
	Tmpl           *templates.Loader
	Providers      *providers.ProviderSet // nil si pas encore configuré
	Reload         func() (*providers.ProviderSet, error)
	TestProvider   func(category, providerType string, cfg map[string]string) (string, error)
	AvailableTypes map[string][]string
	CfgPath        string
	RBAC           *rbac.Store
	RBACPath       string
	CostCfg        costs.Config
}

func NewServer(opts ServerOptions) *Server {
	rbacStore := opts.RBAC
	if rbacStore == nil {
		rbacStore = rbac.NewStore(rbac.Default())
	}
	s := &Server{
		store:          opts.Store,
		db:             opts.DB,
		auth:           opts.Auth,
		devMode:        opts.DevMode,
		tmpl:           opts.Tmpl,
		pvds:           opts.Providers,
		reload:         opts.Reload,
		testProvider:   opts.TestProvider,
		availableTypes: opts.AvailableTypes,
		cfgPath:        opts.CfgPath,
		rbac:           rbacStore,
		rbacPath:       opts.RBACPath,
		costCfg:        opts.CostCfg,
	}
	r := chi.NewRouter()
	s.router = r
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Use(corsMiddleware)

	r.Get("/healthz", s.healthz)
	// Webhook GitLab — public, sécurisé par GITLAB_WEBHOOK_SECRET si configuré
	r.Post("/api/v1/webhooks/gitlab", s.gitlabWebhook)

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
		} else if s.devMode {
			r.Use(s.devModeUserMiddleware)
		}

		r.Get("/api/v1/auth/me", s.me)

		// Setup wizard — /status accessible à tous ; /config, /test, /save, /reload réservés admin
		r.Get("/api/v1/setup/status", s.setupStatus)
		r.With(s.requireRole(rbac.RoleAdmin)).Get("/api/v1/setup/config", s.setupConfig)
		r.With(s.requireRole(rbac.RoleAdmin)).Post("/api/v1/setup/test", s.setupTest)
		r.With(s.requireRole(rbac.RoleAdmin)).Post("/api/v1/setup/save", s.setupSave)
		r.With(s.requireRole(rbac.RoleAdmin)).Post("/api/v1/config/reload", s.reloadConfig)
		r.With(s.requireRole(rbac.RoleAdmin)).Post("/api/v1/rbac/reload", s.reloadRBAC)

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
		r.Get("/api/v1/projects/{name}/branches", s.listProjectBranches)
		r.Get("/api/v1/projects/{name}/images", s.listProjectImages)
		r.With(s.requireRole(rbac.RoleDeveloper)).Get("/api/v1/repos", s.listRepos)
		r.With(s.requireRole(rbac.RoleDeveloper)).Get("/api/v1/namespaces", s.listNamespaces)

		// Groupes projet (voir config/rbac.yaml project_groups — question
		// ouverte n°2 de CLAUDE.md). Lecture = developer+ (alimente le
		// sélecteur de création de projet) ; transfert de propriété = admin.
		r.With(s.requireRole(rbac.RoleDeveloper)).Get("/api/v1/groups", s.listGroups)
		r.With(s.requireRole(rbac.RoleAdmin)).Put("/api/v1/projects/{name}/group", s.transferProjectGroup)

		// Recettes (lecture = viewer+, création/destruction = developer+)
		r.Get("/api/v1/projects/{name}/recettes", s.listRecettes)
		r.With(s.requireRole(rbac.RoleDeveloper)).Post("/api/v1/projects/{name}/recettes", s.createRecette)
		r.With(s.requireRole(rbac.RoleDeveloper)).Delete("/api/v1/projects/{name}/recettes/{recette}", s.destroyRecette)

		// Pipelines (lecture = viewer+, déclenchement = lead+ — voir G1 dans
		// backlog.md : sans variable, le golden path route tout pipeline
		// déclenché par Symphony vers le job "deploy", donc ce endpoint peut
		// déployer en prod au même titre que POST /deployments et doit être
		// gardé au même niveau, pas à developer+)
		r.With(s.requireRole(rbac.RoleLead)).Post("/api/v1/pipelines/trigger", s.triggerPipelineHandler)
		r.Post("/api/v1/pipelines/status", s.getPipelineStatusHandler)
		r.Get("/api/v1/pipelines", s.listAllPipelinesHandler)
		r.Get("/api/v1/pipelines/{project}", s.listPipelinesHandler)

		// Déploiements (lecture = viewer+, création/arrêt = lead+)
		r.Get("/api/v1/deployments", s.listDeployments)
		r.With(s.requireRole(rbac.RoleLead)).Post("/api/v1/deployments", s.deployProject)
		r.With(s.requireRole(rbac.RoleLead)).Delete("/api/v1/deployments/{id}", s.stopDeployment)

		// Inventaire des ressources actives (lecture = viewer+)
		r.Get("/api/v1/inventory", s.getInventory)

		// Coûts par projet / équipe (lecture = viewer+)
		r.Get("/api/v1/costs", s.getCosts)

		// Audit (admin uniquement pour le MVP — audit_log.resource est du
		// texte libre, un filtrage par groupe ne serait pas fiable ; à revoir
		// si la table gagne une vraie colonne project_name)
		r.With(s.requireRole(rbac.RoleAdmin)).Get("/api/v1/audit", s.listAudit)
	})

	return s
}

// devModeUserMiddleware injecte un admin de confort pour SYMPHONY_DEV_MODE=1
// (dev local uniquement, jamais en production). Méthode sur Server (pas une
// fonction libre) pour résoudre ProjectGroups depuis le même *rbac.Store que
// le chemin OIDC — un flag de dev ne doit jamais dispenser d'exercer le
// wizard RBAC/groupes, voir CLAUDE.md section Sécurité (bug setupStatus).
func (s *Server) devModeUserMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		groups := []string{"admin"}
		devUser := &auth.User{
			Sub:           "dev",
			Email:         "dev@localhost",
			Name:          "Dev Mode",
			Groups:        groups,
			Role:          rbac.RoleAdmin,
			IsAdmin:       true,
			ProjectGroups: s.getRBAC().ProjectGroups(groups),
		}
		next.ServeHTTP(w, r.WithContext(auth.WithUser(r.Context(), devUser)))
	})
}

func (s *Server) requireRole(min rbac.Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
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

func (s *Server) getRBAC() *rbac.Manager {
	return s.rbac.Get()
}

// authorizeProject écrit la réponse et retourne false si l'appelant n'a pas
// accès au projet p — à appeler juste après CHAQUE GetProject/
// GetProjectByRepoPath dans un handler qui agit sur un projet précis (jamais
// un middleware de route : les endpoints les plus sensibles — /deployments,
// /pipelines/trigger — ne portent pas le nom du projet dans l'URL, voir G1
// dans documentation/backlog.md pour la classe de bug que ça a produite une
// fois). Un non-membre reçoit 404 (ne jamais révéler l'existence d'un projet
// d'une autre équipe) ; un rôle insuffisant reste géré par requireRole (403).
func (s *Server) authorizeProject(w http.ResponseWriter, r *http.Request, p *database.Project) bool {
	if !s.getRBAC().GroupsEnabled() {
		return true
	}
	user, ok := auth.UserFromContext(r.Context())
	if !ok {
		respond(w, http.StatusNotFound, map[string]string{"error": "projet introuvable"})
		return false
	}
	if user.Role.AtLeast(rbac.RoleAdmin) {
		return true
	}
	if p.OwnerGroup != "" {
		for _, g := range user.ProjectGroups {
			if g == p.OwnerGroup {
				return true
			}
		}
	}
	respond(w, http.StatusNotFound, map[string]string{"error": "projet introuvable"})
	return false
}
