package auth

import (
	"testing"

	"github.com/yourorg/symphony/internal/rbac"
)

func makeProvider(adminGroups, developerGroups []string) *Provider {
	cfg := rbac.Config{Roles: map[string]rbac.RoleConfig{
		"admin":     {Groups: adminGroups},
		"developer": {Groups: developerGroups},
	}}
	return &Provider{rbac: rbac.New(cfg)}
}

func TestIsAdmin_GroupMatch(t *testing.T) {
	p := makeProvider([]string{"platform-admins"}, nil)
	if p.rbac.ResolveRole([]string{"devs", "platform-admins"}) != rbac.RoleAdmin {
		t.Error("user in admin group should be admin")
	}
}

func TestIsAdmin_NoGroupMatch(t *testing.T) {
	p := makeProvider([]string{"platform-admins"}, nil)
	if p.rbac.ResolveRole([]string{"devs", "viewers"}) == rbac.RoleAdmin {
		t.Error("user not in admin group should not be admin")
	}
}

func TestIsAdmin_NoAdminGroupsConfigured_NobodyIsAdmin(t *testing.T) {
	p := &Provider{rbac: rbac.Default()}
	if p.rbac.ResolveRole([]string{"platform-admins", "devs"}) == rbac.RoleAdmin {
		t.Error("nobody should be admin when no admin groups are configured")
	}
}

func TestCanDeploy_Admin(t *testing.T) {
	p := makeProvider([]string{"ops"}, []string{"devs"})
	if !p.rbac.ResolveRole([]string{"ops"}).AtLeast(rbac.RoleDeveloper) {
		t.Error("admin should always be able to deploy")
	}
}

func TestCanDeploy_NoDeveloperGroups_PermissiveDefault(t *testing.T) {
	p := makeProvider(nil, nil)
	if !p.rbac.ResolveRole([]string{}).AtLeast(rbac.RoleDeveloper) {
		t.Error("any authenticated user should deploy when developer groups are empty")
	}
}

func TestCanDeploy_GroupMatch(t *testing.T) {
	p := makeProvider(nil, []string{"devs"})
	if !p.rbac.ResolveRole([]string{"devs"}).AtLeast(rbac.RoleDeveloper) {
		t.Error("user in authorised group should be able to deploy")
	}
}

func TestCanDeploy_GroupNoMatch(t *testing.T) {
	p := makeProvider(nil, []string{"devs"})
	if p.rbac.ResolveRole([]string{"viewers"}).AtLeast(rbac.RoleDeveloper) {
		t.Error("user not in authorised group should not be able to deploy")
	}
}

func TestCanDeploy_MultipleGroups(t *testing.T) {
	p := makeProvider(nil, []string{"devs"})
	if !p.rbac.ResolveRole([]string{"viewers", "devs", "other"}).AtLeast(rbac.RoleDeveloper) {
		t.Error("user with one matching group should be able to deploy")
	}
}
