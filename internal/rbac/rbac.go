package rbac

import (
	"os"
	"sync"

	"gopkg.in/yaml.v3"
)

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleLead      Role = "lead"      // chef de projet : deploy prod + toutes les actions developer
	RoleDeveloper Role = "developer" // crée des projets, trigger CI, déploie en recette
	RoleViewer    Role = "viewer"    // lecture seule
)

var hierarchy = map[Role]int{
	RoleAdmin:     3,
	RoleLead:      2,
	RoleDeveloper: 1,
	RoleViewer:    0,
}

// AtLeast returns true if r has at least the privileges of min.
func (r Role) AtLeast(min Role) bool {
	return hierarchy[r] >= hierarchy[min]
}

type Config struct {
	Roles         map[string]RoleConfig `yaml:"roles"`
	ProjectGroups []ProjectGroup        `yaml:"project_groups"`
}

type RoleConfig struct {
	Groups []string `yaml:"groups"`
}

// ProjectGroup est une équipe telle que Symphony la connaît : un nom stable
// (celui stocké dans projects.owner_group) mappé sur un ou plusieurs claims
// de groupe OIDC. L'indirection nom↔claims n'est pas du confort : certains
// IdP (Azure AD) émettent des object IDs illisibles dans la claim "groups",
// et une même équipe peut regrouper plusieurs claims (devs + leads).
type ProjectGroup struct {
	Name        string   `yaml:"name" json:"name"`
	Description string   `yaml:"description" json:"description,omitempty"`
	OIDCGroups  []string `yaml:"oidc_groups" json:"oidc_groups,omitempty"`
}

// Manager resolves an authenticated user's role and project groups from
// their OIDC groups.
type Manager struct {
	adminGroups     map[string]bool
	leadGroups      map[string]bool
	developerGroups map[string]bool
	projectGroups   []ProjectGroup
	oidcToProject   map[string][]string // claim OIDC -> noms de groupes Symphony
}

func New(cfg Config) *Manager {
	m := &Manager{
		adminGroups:     map[string]bool{},
		leadGroups:      map[string]bool{},
		developerGroups: map[string]bool{},
		projectGroups:   cfg.ProjectGroups,
		oidcToProject:   map[string][]string{},
	}
	for _, g := range cfg.Roles["admin"].Groups {
		m.adminGroups[g] = true
	}
	for _, g := range cfg.Roles["lead"].Groups {
		m.leadGroups[g] = true
	}
	for _, g := range cfg.Roles["developer"].Groups {
		m.developerGroups[g] = true
	}
	for _, pg := range cfg.ProjectGroups {
		for _, og := range pg.OIDCGroups {
			m.oidcToProject[og] = append(m.oidcToProject[og], pg.Name)
		}
	}
	return m
}

// Default returns a permissive manager: nobody is admin or lead by default,
// no developer groups = every authenticated user is treated as developer,
// no project groups = project-level scoping is disabled entirely.
func Default() *Manager {
	return &Manager{
		adminGroups:     map[string]bool{},
		leadGroups:      map[string]bool{},
		developerGroups: map[string]bool{},
		oidcToProject:   map[string]([]string){},
	}
}

// ResolveRole maps OIDC groups to the highest applicable role.
//
// Priority order: admin > lead > developer > viewer.
// Admin and lead groups must be explicitly configured.
// If no developer groups are configured, every authenticated user is a
// developer (permissive default for small teams that don't need RBAC yet).
func (m *Manager) ResolveRole(groups []string) Role {
	for _, g := range groups {
		if m.adminGroups[g] {
			return RoleAdmin
		}
	}
	for _, g := range groups {
		if m.leadGroups[g] {
			return RoleLead
		}
	}
	if len(m.developerGroups) == 0 {
		return RoleDeveloper
	}
	for _, g := range groups {
		if m.developerGroups[g] {
			return RoleDeveloper
		}
	}
	return RoleViewer
}

// GroupsEnabled reports whether project-level group scoping is active.
// Opt-in, tout ou rien : project_groups absent/vide (le cas par défaut,
// et celui de toute installation antérieure à cette fonctionnalité) désactive
// entièrement la dimension groupe — tout developer/lead agit sur tous les
// projets, comme avant. Un catalogue non vide l'active partout à la fois :
// jamais de demi-activation par projet, ce qui serait inauditable.
func (m *Manager) GroupsEnabled() bool {
	return len(m.projectGroups) > 0
}

// ProjectGroups traduit les claims de groupe OIDC d'un utilisateur en noms
// de groupes Symphony (dédupliqués). Ne fait aucun appel réseau ni cache :
// recalculé à chaque requête à partir du token, jamais mémorisé.
func (m *Manager) ProjectGroups(oidcGroups []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, og := range oidcGroups {
		for _, pg := range m.oidcToProject[og] {
			if !seen[pg] {
				seen[pg] = true
				out = append(out, pg)
			}
		}
	}
	return out
}

// KnownGroup reports whether name is a project group Symphony knows about
// (catalogue admin-managed dans config/rbac.yaml).
func (m *Manager) KnownGroup(name string) bool {
	for _, pg := range m.projectGroups {
		if pg.Name == name {
			return true
		}
	}
	return false
}

// AllProjectGroups retourne le catalogue complet — réservé aux appelants
// admin (GET /api/v1/groups les filtre déjà par appartenance pour les autres).
func (m *Manager) AllProjectGroups() []ProjectGroup {
	return m.projectGroups
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Config{}, err
	}
	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return Config{}, err
	}
	if cfg.Roles == nil {
		cfg.Roles = map[string]RoleConfig{}
	}
	return cfg, nil
}

// Store rend un *Manager rechargeable à chaud sans redémarrer le process
// (voir POST /api/v1/rbac/reload) : un seul exemplaire canonique, partagé
// entre le middleware d'auth OIDC et les handlers API qui vérifient
// l'appartenance à un groupe projet, pour que les deux ne divergent jamais
// après un reload.
type Store struct {
	mu sync.RWMutex
	m  *Manager
}

func NewStore(m *Manager) *Store {
	if m == nil {
		m = Default()
	}
	return &Store{m: m}
}

func (s *Store) Get() *Manager {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.m
}

func (s *Store) Set(m *Manager) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.m = m
}
