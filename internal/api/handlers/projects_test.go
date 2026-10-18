package handlers

import (
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	gitlabci "github.com/clemuscle/symphony/internal/provider/ci/gitlabci"
	argocdrt "github.com/clemuscle/symphony/internal/provider/runtime/argocd"
	dockerrt "github.com/clemuscle/symphony/internal/provider/runtime/docker"
)

func TestCatalogueFile(t *testing.T) {
	stack := &gitops.Stack{ID: "api-rest-go", Language: "go", Type: "api"}
	repo := &provider.Repo{WebURL: "http://gitlab.local/apps/payment-api"}
	ci := gitlabci.New("gitlab-ci", "http://gitlab.local", "")
	p := createParams{CreateProjectRequest: CreateProjectRequest{
		Name: "payment-api", Owner: "team-payments", Tier: "critical", Lifecycle: "production",
	}}

	// Sans fiche dans la stack : fiche minimale et action par défaut
	content, err := catalogueFile("", p, stack, repo, ci, "bob", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	fiche, err := gitops.ParseFiche(content, "payment-api")
	if err != nil {
		t.Fatalf("fiche illisible : %v\n%s", err, content)
	}
	// Fiche Backstage : un Component, l'équipe en propriétaire
	if fiche.Legacy || !strings.Contains(content, "apiVersion: backstage.io/v1alpha1\nkind: Component\n") ||
		!strings.Contains(content, "owner: team-payments") {
		t.Errorf("fiche au format Backstage attendue :\n%s", content)
	}
	svc := fiche.Service
	m := svc.Metadata
	if m.Owner != "team-payments" || m.Tier != "critical" || m.Lifecycle != "production" || m.Labels["stack"] != "api-rest-go" {
		t.Errorf("metadata : %+v", m)
	}
	if svc.Spec.Repo != repo.WebURL || len(svc.Spec.Links) != 2 {
		t.Errorf("spec : %+v", svc.Spec)
	}
	if len(svc.Spec.Actions) != 1 || svc.Spec.Actions[0].CIProvider != "gitlab-ci" {
		t.Errorf("actions : %+v", svc.Spec.Actions)
	}
	if strings.Contains(content, "slo:") || strings.Contains(content, "team:") || strings.Contains(content, "kubernetes:") {
		t.Errorf("champs vides écrits dans la fiche :\n%s", content)
	}

	// Fiche de la stack : ses liens et actions sont gardés, le formulaire
	// l'emporte sur ses valeurs ; les manifests Kubernetes de la stack y sont
	base := "kind: Service\nmetadata:\n  owner: team-x\n  tier: internal\nspec:\n" +
		"  links:\n    - title: Runbook\n      url: http://wiki/runbook\n" +
		"  actions:\n    - name: Scaler\n      inputs:\n        - id: REPLICAS\n          type: integer\n"
	p.Owner = ""
	content, err = catalogueFile(base, p, stack, repo, ci, "bob", &catalogue.Kubernetes{Helm: "deploy/chart"},
		&catalogue.Infra{Kubernetes: "deploy/infra", Env: map[string]catalogue.InfraSecretRef{"DATABASE_URL": {Secret: "db-app", Key: "uri"}}})
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
	if k := svc.Spec.Kubernetes; k == nil || k.Helm != "deploy/chart" {
		t.Errorf("kubernetes : %+v", k)
	}
	if in := svc.Spec.Infra; in == nil || in.Env["DATABASE_URL"].Secret != "db-app" {
		t.Errorf("infra : %+v", in)
	}

	if _, err := catalogueFile("kind: [", p, stack, repo, ci, "bob", nil, nil); err == nil {
		t.Error("fiche invalide de la stack acceptée")
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

// Une stack avec infra n'est acceptée que si tous les environnements du
// projet tournent sur Kubernetes
func TestCheckInfraRuntimes(t *testing.T) {
	providers := provider.NewRegistry()
	providers.RegisterRuntime(dockerrt.New("docker", "/var/run/docker.sock"), true)
	argo, err := argocdrt.New("k8s", nil, argocdrt.Config{URL: "http://argocd", Repo: "p/d", RepoURL: "http://g/p/d.git"})
	if err != nil {
		t.Fatal(err)
	}
	providers.RegisterRuntime(argo, false)
	stack := &gitops.Stack{ID: "db", RTProvider: "docker",
		Infra: &catalogue.Infra{Kubernetes: "deploy/infra"},
		Files: []gitops.FileRule{{Path: "deploy/infra", When: gitops.When{"postgres": true}}}}
	envs := environments.NewHolder([]environments.Environment{{Name: "staging", Runtime: "docker"}, {Name: "production", Runtime: "k8s"}})
	req := CreateProjectRequest{Parameters: map[string]any{"postgres": true}}
	if err := checkInfraRuntimes(providers, envs, stack, req); err == nil || !strings.Contains(err.Error(), "staging tourne sur docker") {
		t.Errorf("staging sur Docker : %v", err)
	}
	stack.Environments = map[string]string{"staging": "k8s"}
	if err := checkInfraRuntimes(providers, envs, stack, req); err != nil {
		t.Errorf("tout sur Kubernetes : %v", err)
	}
	// Sans l'option, pas d'infra : Docker convient
	stack.Environments = nil
	req.Parameters["postgres"] = false
	if err := checkInfraRuntimes(providers, envs, stack, req); err != nil {
		t.Errorf("sans infra : %v", err)
	}
	// Sans environnements : le runtime du projet
	req.Parameters["postgres"] = true
	if err := checkInfraRuntimes(providers, environments.NewHolder(nil), stack, req); err == nil {
		t.Error("runtime docker sans environnements accepté")
	}
}
