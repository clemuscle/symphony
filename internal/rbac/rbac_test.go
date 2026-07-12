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
