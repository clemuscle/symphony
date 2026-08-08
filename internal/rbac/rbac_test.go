package rbac

import "testing"

func TestResolveRole_Admin(t *testing.T) {
	m := New(Config{Roles: map[string]RoleConfig{
		"admin":     {Groups: []string{"platform-admins"}},
		"developer": {Groups: []string{"devs"}},
	}})
	if r := m.ResolveRole([]string{"devs", "platform-admins"}); r != RoleAdmin {
		t.Errorf("got %s, want admin", r)
	}
}

func TestResolveRole_Developer(t *testing.T) {
	m := New(Config{Roles: map[string]RoleConfig{
		"admin":     {Groups: []string{"platform-admins"}},
		"developer": {Groups: []string{"devs"}},
	}})
	if r := m.ResolveRole([]string{"devs"}); r != RoleDeveloper {
		t.Errorf("got %s, want developer", r)
	}
}

func TestResolveRole_Viewer(t *testing.T) {
	m := New(Config{Roles: map[string]RoleConfig{
		"admin":     {Groups: []string{"platform-admins"}},
		"developer": {Groups: []string{"devs"}},
	}})
	if r := m.ResolveRole([]string{"viewers"}); r != RoleViewer {
		t.Errorf("got %s, want viewer", r)
	}
}

func TestResolveRole_NoDeveloperGroups_PermissiveDefault(t *testing.T) {
	m := New(Config{Roles: map[string]RoleConfig{
		"admin": {Groups: []string{"platform-admins"}},
	}})
	// No developer groups → every authenticated user is a developer
	if r := m.ResolveRole([]string{"anyone"}); r != RoleDeveloper {
		t.Errorf("got %s, want developer (permissive default)", r)
	}
}

func TestResolveRole_NoAdminGroups_NobodyIsAdmin(t *testing.T) {
	m := Default()
	if r := m.ResolveRole([]string{"platform-admins"}); r == RoleAdmin {
		t.Error("no admin groups configured → nobody should be admin")
	}
}

func TestGroupsEnabled_EmptyByDefault(t *testing.T) {
	if Default().GroupsEnabled() {
		t.Error("no project_groups configured → scoping should stay disabled")
	}
	m := New(Config{Roles: map[string]RoleConfig{}})
	if m.GroupsEnabled() {
		t.Error("empty project_groups → scoping should stay disabled")
	}
}

func TestGroupsEnabled_NonEmpty(t *testing.T) {
	m := New(Config{ProjectGroups: []ProjectGroup{
		{Name: "team-paiements", OIDCGroups: []string{"paiements-devs"}},
	}})
	if !m.GroupsEnabled() {
		t.Error("non-empty project_groups → scoping should be enabled")
	}
}

func TestProjectGroups_MapsOIDCClaimsToSymphonyNames(t *testing.T) {
	m := New(Config{ProjectGroups: []ProjectGroup{
		{Name: "team-paiements", OIDCGroups: []string{"paiements-devs", "paiements-leads"}},
		{Name: "team-plateforme", OIDCGroups: []string{"devops-team"}},
	}})
	got := m.ProjectGroups([]string{"paiements-devs", "someone-else"})
	if len(got) != 1 || got[0] != "team-paiements" {
		t.Errorf("got %v, want [team-paiements]", got)
	}
}

func TestProjectGroups_DedupesMultipleClaimsSameGroup(t *testing.T) {
	m := New(Config{ProjectGroups: []ProjectGroup{
		{Name: "team-paiements", OIDCGroups: []string{"paiements-devs", "paiements-leads"}},
	}})
	got := m.ProjectGroups([]string{"paiements-devs", "paiements-leads"})
	if len(got) != 1 || got[0] != "team-paiements" {
		t.Errorf("got %v, want [team-paiements] deduped", got)
	}
}

func TestKnownGroup(t *testing.T) {
	m := New(Config{ProjectGroups: []ProjectGroup{
		{Name: "team-paiements", OIDCGroups: []string{"paiements-devs"}},
	}})
	if !m.KnownGroup("team-paiements") {
		t.Error("team-paiements should be known")
	}
	if m.KnownGroup("team-inexistante") {
		t.Error("team-inexistante should not be known")
	}
}

func TestStore_GetSet(t *testing.T) {
	s := NewStore(Default())
	if s.Get().GroupsEnabled() {
		t.Fatal("expected disabled by default")
	}
	s.Set(New(Config{ProjectGroups: []ProjectGroup{{Name: "team-a", OIDCGroups: []string{"a"}}}}))
	if !s.Get().GroupsEnabled() {
		t.Error("expected enabled after Set")
	}
}

func TestAtLeast(t *testing.T) {
	if !RoleAdmin.AtLeast(RoleAdmin) {
		t.Error("admin >= admin")
	}
	if !RoleAdmin.AtLeast(RoleDeveloper) {
		t.Error("admin >= developer")
	}
	if !RoleAdmin.AtLeast(RoleViewer) {
		t.Error("admin >= viewer")
	}
	if RoleDeveloper.AtLeast(RoleAdmin) {
		t.Error("developer < admin")
	}
	if !RoleDeveloper.AtLeast(RoleViewer) {
		t.Error("developer >= viewer")
	}
	if RoleViewer.AtLeast(RoleDeveloper) {
		t.Error("viewer < developer")
	}
}
