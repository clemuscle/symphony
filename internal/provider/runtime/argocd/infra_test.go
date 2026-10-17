package argocd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
)

// testInfra : un PostgreSQL CloudNativePG, comme celui des stacks officielles
var testInfra = &provider.Infra{
	Files: map[string]string{
		"cluster.yaml": `apiVersion: postgresql.cnpg.io/v1
kind: Cluster
metadata:
  name: db
spec:
  instances: 1
  storage: {size: 1Gi}
`,
		"README.md": "pas un manifest",
	},
	Env: map[string]provider.InfraSecretRef{"DATABASE_URL": {Secret: "db-app", Key: "uri"}},
}

func TestDeployInfraGenerated(t *testing.T) {
	p, scm, argo := newTest(t)
	req := provider.DeployRequest{Name: "mon-api-staging", Project: "mon-api", Environment: "staging",
		Image: "localhost:5050/root/mon-api", Tag: "abc123", Infra: testInfra}
	if _, err := p.Deploy(req); err != nil {
		t.Fatal(err)
	}
	dir := "apps/mon-api-staging/"
	k, _ := parseKustomization(scm.files[dir+"kustomization.yaml"])
	if !contains(k.Resources, "infra") {
		t.Errorf("infra absente des ressources : %v", k.Resources)
	}
	ik := scm.files[dir+"infra/kustomization.yaml"]
	for _, want := range []string{"namePrefix: mon-api-staging-", "- src/cluster.yaml", "Prune=false"} {
		if !strings.Contains(ik, want) {
			t.Errorf("infra/kustomization.yaml sans %q :\n%s", want, ik)
		}
	}
	if strings.Contains(ik, "README") {
		t.Errorf("un fichier non YAML listé en ressource :\n%s", ik)
	}
	dep := scm.files[dir+"deployment.yaml"]
	if !strings.Contains(dep, `name: "mon-api-staging-db-app"`) || !strings.Contains(dep, `key: "uri"`) {
		t.Errorf("DATABASE_URL non lu dans le secret de l'infra :\n%s", dep)
	}
	app := argo.apps["mon-api-staging"]
	if app.Metadata.Annotations[infraAnnotation] != "Cluster" {
		t.Errorf("annotation de l'infra : %v", app.Metadata.Annotations)
	}

	// État de l'infra : les objets de ses types, avec leur santé
	app.Status = &appStatus{Resources: []appResource{{Kind: "Deployment", Name: "mon-api-staging"}, {Kind: "Cluster", Name: "mon-api-staging-db"}}}
	app.Status.Resources[1].Health.Status = "Progressing"
	argo.apps["mon-api-staging"] = app
	app.Status.Summary.Images = []string{"ghcr.io/cloudnative-pg/postgresql:18", "localhost:5050/root/mon-api:abc123"}
	argo.apps["mon-api-staging"] = app
	d, err := p.Status("mon-api-staging")
	if err != nil {
		t.Fatal(err)
	}
	// L'image du service, pas celle de sa base (promotion)
	if d.Image != "localhost:5050/root/mon-api:abc123" {
		t.Errorf("image : %s", d.Image)
	}
	if len(d.Infra) != 1 || d.Infra[0] != (provider.InfraResource{Kind: "Cluster", Name: "mon-api-staging-db", Health: "Progressing"}) {
		t.Errorf("infra : %+v", d.Infra)
	}

	// Stop garde l'infra (ressource et annotation)
	if err := p.Stop("mon-api-staging"); err != nil {
		t.Fatal(err)
	}
	k, _ = parseKustomization(scm.files[dir+"kustomization.yaml"])
	if !contains(k.Resources, "infra") || argo.apps["mon-api-staging"].Metadata.Annotations[infraAnnotation] != "Cluster" {
		t.Errorf("après Stop : %v, %v", k.Resources, argo.apps["mon-api-staging"].Metadata.Annotations)
	}

	// Redéployé sans infra : les fichiers de l'infra quittent Git (l'objet,
	// protégé de l'élagage, reste dans le cluster)
	req.Infra = nil
	if _, err := p.Deploy(req); err != nil {
		t.Fatal(err)
	}
	if _, ok := scm.files[dir+"infra/kustomization.yaml"]; ok {
		t.Error("infra restée dans Git")
	}
}

func TestDeployInfraKustomize(t *testing.T) {
	p, scm, _ := newTest(t)
	req := stackRequest("kustomize", testBase)
	req.SecretEnv = nil
	req.Infra = testInfra
	if _, err := p.Deploy(req); err != nil {
		t.Fatal(err)
	}
	dir := "apps/mon-api-staging/"
	k, _ := parseKustomization(scm.files[dir+"kustomization.yaml"])
	if !contains(k.Resources, "infra") || len(k.Patches) != 1 || !strings.Contains(k.Patches[0].Patch, "mon-api-staging-db-app") {
		t.Errorf("kustomization : %+v", k)
	}
	if strings.Contains(scm.files[dir+"infra/kustomization.yaml"], "namePrefix") {
		t.Error("double préfixe : la base Kustomize préfixe déjà l'infra")
	}
	// Un chart Helm porte lui-même son infra
	h := stackRequest("helm", testChart)
	h.Infra = testInfra
	if _, err := p.Deploy(h); err == nil {
		t.Error("infra avec un chart Helm acceptée")
	}
}

// Rendu réel par kustomize, s'il est installé
func TestInfraRender(t *testing.T) {
	if _, err := exec.LookPath("kustomize"); err != nil {
		t.Skip("kustomize absent")
	}
	for name, req := range map[string]provider.DeployRequest{
		"générés": {Name: "mon-api-staging", Image: "localhost:5050/root/mon-api", Tag: "abc123", Infra: testInfra},
		"kustomize": func() provider.DeployRequest {
			r := stackRequest("kustomize", testBase)
			r.SecretEnv = nil
			r.Infra = testInfra
			return r
		}(),
	} {
		p, scm, _ := newTest(t)
		if _, err := p.Deploy(req); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		for f, c := range scm.files {
			path := filepath.Join(dir, strings.TrimPrefix(f, "apps/mon-api-staging/"))
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte(c), 0o644)
		}
		out, err := exec.Command("kustomize", "build", dir).CombinedOutput()
		if err != nil {
			t.Fatalf("%s : %v\n%s", name, err, out)
		}
		for _, want := range []string{"kind: Cluster", "name: mon-api-staging-db\n", "argocd.argoproj.io/sync-options: Prune=false", "name: mon-api-staging-db-app"} {
			if !strings.Contains(string(out), want) {
				t.Errorf("%s : %q absent du rendu :\n%s", name, want, out)
			}
		}
	}
}

func contains(list []string, v string) bool {
	for _, x := range list {
		if x == v {
			return true
		}
	}
	return false
}
