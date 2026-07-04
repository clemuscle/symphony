package auth

import "testing"

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
