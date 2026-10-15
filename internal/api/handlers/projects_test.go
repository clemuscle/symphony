package handlers

import (
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	gitlabci "github.com/clemuscle/symphony/internal/provider/ci/gitlabci"
)

func TestCatalogueFile(t *testing.T) {
	gp := &gitops.GoldenPath{ID: "api-rest-go", Language: "go", Type: "api"}
	repo := &provider.Repo{WebURL: "http://gitlab.local/apps/payment-api"}
	ci := gitlabci.New("gitlab-ci", "http://gitlab.local", "")
	p := createParams{CreateProjectRequest: CreateProjectRequest{
		Name: "payment-api", Owner: "team-payments", Tier: "critical", Lifecycle: "production",
	}}

	// Sans fiche dans le golden path : fiche minimale et action par défaut
	content, err := catalogueFile("", p, gp, repo, ci, "bob")
	if err != nil {
		t.Fatal(err)
	}
	svc, err := gitops.ParseService("payment-api", content)
	if err != nil {
		t.Fatalf("fiche illisible : %v\n%s", err, content)
	}
	m := svc.Metadata
	if m.Owner != "team-payments" || m.Tier != "critical" || m.Lifecycle != "production" || m.Labels["golden_path"] != "api-rest-go" {
		t.Errorf("metadata : %+v", m)
	}
	if svc.Spec.Repo != repo.WebURL || len(svc.Spec.Links) != 2 {
		t.Errorf("spec : %+v", svc.Spec)
	}
	if len(svc.Spec.Actions) != 1 || svc.Spec.Actions[0].CIProvider != "gitlab-ci" {
		t.Errorf("actions : %+v", svc.Spec.Actions)
	}
	if strings.Contains(content, "slo:") || strings.Contains(content, "team:") {
		t.Errorf("champs vides écrits dans la fiche :\n%s", content)
	}

	// Fiche du golden path : ses liens et actions sont gardés, le formulaire
	// l'emporte sur ses valeurs
	base := "kind: Service\nmetadata:\n  owner: team-x\n  tier: internal\nspec:\n" +
		"  links:\n    - title: Runbook\n      url: http://wiki/runbook\n" +
		"  actions:\n    - name: Scaler\n      inputs:\n        - id: REPLICAS\n          type: integer\n"
	p.Owner = ""
	content, err = catalogueFile(base, p, gp, repo, ci, "bob")
	if err != nil {
		t.Fatal(err)
	}
	svc, _ = gitops.ParseService("payment-api", content)
	if svc.Metadata.Owner != "team-x" || svc.Metadata.Tier != "critical" {
		t.Errorf("fusion metadata : %+v", svc.Metadata)
	}
	if len(svc.Spec.Links) != 3 || svc.Spec.Links[2].Title != "Runbook" {
		t.Errorf("liens : %+v", svc.Spec.Links)
	}
	if len(svc.Spec.Actions) != 1 || svc.Spec.Actions[0].Name != "Scaler" || svc.Spec.Actions[0].CIProvider != "gitlab-ci" {
		t.Errorf("actions : %+v", svc.Spec.Actions)
	}

	if _, err := catalogueFile("kind: [", p, gp, repo, ci, "bob"); err == nil {
		t.Error("fiche invalide du golden path acceptée")
	}
}

func TestProjectTeam(t *testing.T) {
	bob := &auth.User{Name: "bob", Role: auth.RoleDeveloper, Teams: []string{"team-payments"}}
	dev2 := &auth.User{Name: "eve", Role: auth.RoleDeveloper, Teams: []string{"team-a", "team-b"}}
	alone := &auth.User{Name: "dan", Role: auth.RoleDeveloper, Teams: []string{}}
	alice := &auth.User{Name: "alice", Role: auth.RoleAdmin, Teams: []string{"team-platform"}}
	cases := []struct {
		u       *auth.User
		owner   string
		want    string
		wantErr string
	}{
		{bob, "", "team-payments", ""},
		{bob, "team-payments", "team-payments", ""},
		{bob, "team-product", "", "pas membre de l'équipe team-product"},
		{dev2, "", "", "owner requis : l'une de vos équipes (team-a, team-b)"},
		{dev2, "team-b", "team-b", ""},
		{alone, "", "", "membre d'aucune équipe"},
		{alice, "team-product", "team-product", ""},
		{alice, "", "team-platform", ""},
	}
	for i, c := range cases {
		got, err := projectTeam(c.u, c.owner)
		switch {
		case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
			t.Errorf("cas %d : erreur = %v, attendu %q", i, err, c.wantErr)
		case c.wantErr == "" && (err != nil || got != c.want):
			t.Errorf("cas %d : %q, %v ; attendu %q", i, got, err, c.want)
		}
	}
}
