package auth

import "testing"

func TestIsAdmin_GroupMatch(t *testing.T) {
	p := &Provider{adminGroups: map[string]bool{"platform-admins": true}}
	// Simuler le calcul isAdmin tel qu'il est fait dans Middleware
	groups := []string{"devs", "platform-admins"}
	isAdmin := false
	for _, g := range groups {
		if p.adminGroups[g] {
			isAdmin = true
			break
		}
	}
	if !isAdmin {
		t.Error("user in admin group should be admin")
	}
}

func TestIsAdmin_NoGroupMatch(t *testing.T) {
	p := &Provider{adminGroups: map[string]bool{"platform-admins": true}}
	groups := []string{"devs", "viewers"}
	isAdmin := false
	for _, g := range groups {
		if p.adminGroups[g] {
			isAdmin = true
			break
		}
	}
	if isAdmin {
		t.Error("user not in admin group should not be admin")
	}
}

func TestIsAdmin_NoAdminGroupsConfigured_NobodyIsAdmin(t *testing.T) {
	p := &Provider{adminGroups: map[string]bool{}}
	groups := []string{"platform-admins", "devs"}
	isAdmin := false
	if len(p.adminGroups) != 0 {
		for _, g := range groups {
			if p.adminGroups[g] {
				isAdmin = true
				break
			}
		}
	}
	if isAdmin {
		t.Error("nobody should be admin when ADMIN_GROUPS is empty")
	}
}

func TestCanDeploy_Admin(t *testing.T) {
	p := &Provider{deployerGroups: map[string]bool{"ops": true}}
	user := &User{IsAdmin: true, Groups: []string{}}
	if !p.CanDeploy(user) {
		t.Error("admin should always be able to deploy")
	}
}

func TestCanDeploy_NoGroups_PermissiveDefault(t *testing.T) {
	p := &Provider{deployerGroups: map[string]bool{}} // empty = permissive
	user := &User{IsAdmin: false, Groups: []string{}}
	if !p.CanDeploy(user) {
		t.Error("any authenticated user should deploy when DEPLOYER_GROUPS is empty")
	}
}

func TestCanDeploy_GroupMatch(t *testing.T) {
	p := &Provider{deployerGroups: map[string]bool{"devs": true}}
	user := &User{IsAdmin: false, Groups: []string{"devs"}}
	if !p.CanDeploy(user) {
		t.Error("user in authorised group should be able to deploy")
	}
}

func TestCanDeploy_GroupNoMatch(t *testing.T) {
	p := &Provider{deployerGroups: map[string]bool{"devs": true}}
	user := &User{IsAdmin: false, Groups: []string{"viewers"}}
	if p.CanDeploy(user) {
		t.Error("user not in authorised group should not be able to deploy")
	}
}

func TestCanDeploy_MultipleGroups(t *testing.T) {
	p := &Provider{deployerGroups: map[string]bool{"devs": true}}
	user := &User{IsAdmin: false, Groups: []string{"viewers", "devs", "other"}}
	if !p.CanDeploy(user) {
		t.Error("user with one matching group should be able to deploy")
	}
}
