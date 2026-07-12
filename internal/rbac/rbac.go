package rbac

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Role string

const (
	RoleAdmin     Role = "admin"
	RoleDeveloper Role = "developer"
	RoleViewer    Role = "viewer"
)

var hierarchy = map[Role]int{
	RoleAdmin:     2,
	RoleDeveloper: 1,
	RoleViewer:    0,
}

// AtLeast returns true if r has at least the privileges of min.
func (r Role) AtLeast(min Role) bool {
	return hierarchy[r] >= hierarchy[min]
}

type Config struct {
	Roles map[string]RoleConfig `yaml:"roles"`
}

type RoleConfig struct {
	Groups []string `yaml:"groups"`
}

// Manager resolves an authenticated user's role from their OIDC groups.
type Manager struct {
	adminGroups     map[string]bool
	developerGroups map[string]bool
}

func New(cfg Config) *Manager {
	m := &Manager{
		adminGroups:     map[string]bool{},
		developerGroups: map[string]bool{},
	}
	for _, g := range cfg.Roles["admin"].Groups {
		m.adminGroups[g] = true
	}
	for _, g := range cfg.Roles["developer"].Groups {
		m.developerGroups[g] = true
	}
	return m
}

// Default returns a permissive manager: no admin groups (nobody is admin),
// and no developer groups (everyone authenticated is a developer).
// Used when no rbac.yaml is present — safe default for small teams.
func Default() *Manager {
	return &Manager{
		adminGroups:     map[string]bool{},
		developerGroups: map[string]bool{},
	}
}

// ResolveRole maps OIDC groups to the highest applicable role.
//
// Admin groups must be explicitly configured — nobody is admin by default.
// If no developer groups are configured, every authenticated user is a
// developer (permissive default for small teams that don't need RBAC yet).
func (m *Manager) ResolveRole(groups []string) Role {
	for _, g := range groups {
		if m.adminGroups[g] {
			return RoleAdmin
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
