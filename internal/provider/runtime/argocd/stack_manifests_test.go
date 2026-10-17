package argocd

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
)

// Chart et base de test, au contrat de documentation/reference/stack.yaml
var (
	testChart = map[string]string{
		"Chart.yaml":  "apiVersion: v2\nname: mon-api\nversion: 0.1.0\n",
		"values.yaml": "replicaCount: 1\n",
		"templates/deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{ .Values.fullnameOverride }}
spec:
  replicas: {{ .Values.replicaCount }}
  selector: {matchLabels: {app: {{ .Values.fullnameOverride }}}}
  template:
    metadata:
      labels: {app: {{ .Values.fullnameOverride }}}
      annotations: {{ toYaml .Values.podAnnotations | nindent 8 }}
    spec:
      containers:
        - name: app
          image: "{{ .Values.image.repository }}:{{ .Values.image.tag }}"
          {{- with .Values.secretEnv }}
          envFrom: [{secretRef: {name: {{ . }}}}]
          {{- end }}
`,
	}
	testBase = map[string]string{
		"kustomization.yaml": "resources: [deployment.yaml, service.yaml]\n",
		"deployment.yaml": `apiVersion: apps/v1
kind: Deployment
metadata: {name: app}
spec:
  selector: {matchLabels: {app.kubernetes.io/name: app}}
  template:
    metadata: {labels: {app.kubernetes.io/name: app}}
    spec:
      containers:
        - name: app
          image: app
          ports: [{name: http, containerPort: 8080}]
`,
		"service.yaml": `apiVersion: v1
kind: Service
metadata: {name: app}
spec:
  selector: {app.kubernetes.io/name: app}
  ports: [{name: http, port: 80, targetPort: http}]
`,
	}
	testSecrets = map[string]provider.SecretRef{"DB_PASSWORD": {Provider: "vault", Path: "apps/mon-api/db", Key: "password"}}
)

func stackRequest(kind string, files map[string]string) provider.DeployRequest {
	return provider.DeployRequest{
		Name:      "mon-api-staging",
		Project:   "mon-api",
		Image:     "localhost:5050/root/mon-api",
		Tag:       "abc123",
		Port:      3000,
		EnvVars:   map[string]string{"A": "un", "B": "deux: points"},
		SecretEnv: testSecrets,
		Manifests: &provider.Manifests{Kind: kind, Files: files},
	}
}

func newSecretTest(t *testing.T) (*Provider, *fakeSCM, *fakeArgo) {
	p, scm, argo := newTest(t)
	p.cfg.SecretStore, p.cfg.SecretStoreProvider = "symphony-vault", "vault"
	return p, scm, argo
}

func TestDeployHelm(t *testing.T) {
	p, scm, argo := newSecretTest(t)
	// Déploiement précédent avec les manifests générés : retirés du dossier
	req := stackRequest("helm", testChart)
	gen := req
	gen.Manifests, gen.SecretEnv = nil, nil
	if _, err := p.Deploy(gen); err != nil {
		t.Fatal(err)
	}
	if _, err := p.Deploy(req); err != nil {
		t.Fatal(err)
	}
	dir := "apps/mon-api-staging/"
	for _, f := range []string{"chart/Chart.yaml", "chart/templates/deployment.yaml", "chart/" + helmSecretFile, "values.yaml"} {
		if _, ok := scm.files[dir+f]; !ok {
			t.Errorf("%s absent ; fichiers : %v", f, keys(scm.files))
		}
	}
	for _, f := range []string{"kustomization.yaml", "deployment.yaml", "service.yaml", "ingress.yaml"} {
		if _, ok := scm.files[dir+f]; ok {
			t.Errorf("%s du déploiement précédent pas retiré", f)
		}
	}
	v, err := parseHelmValues(scm.files[dir+"values.yaml"])
	if err != nil {
		t.Fatal(err)
	}
	if v.FullnameOverride != "mon-api-staging" || v.Image.Repository != "localhost:5050/root/mon-api" || v.Image.Tag != "abc123" ||
		v.ReplicaCount != 1 || v.ContainerPort != 3000 || v.Env["B"] != "deux: points" || v.SecretEnv != "mon-api-staging-env" ||
		!v.Ingress.Enabled || v.Ingress.Host != "mon-api-staging.localhost" {
		t.Errorf("values = %+v", v)
	}
	// Les secrets : des références seulement, en chaîne brute pour Helm
	secret := scm.files[dir+"chart/"+helmSecretFile]
	if !strings.HasPrefix(secret, "{{`") || !strings.Contains(secret, `key: "apps/mon-api/db"`) {
		t.Errorf("ExternalSecret :\n%s", secret)
	}
	app := argo.apps["mon-api-staging"]
	if app.Spec.Source.Path != "apps/mon-api-staging/chart" || app.Spec.Source.Helm == nil ||
		app.Spec.Source.Helm.ValueFiles[0] != "../values.yaml" {
		t.Errorf("source de l'Application : %+v", app.Spec.Source)
	}

	// Stop et Restart changent values.yaml, et gardent la source Helm
	if err := p.Stop("mon-api-staging"); err != nil {
		t.Fatal(err)
	}
	if v, _ := parseHelmValues(scm.files[dir+"values.yaml"]); v.ReplicaCount != 0 || v.Image.Tag != "abc123" {
		t.Errorf("après Stop : %+v", v)
	}
	if err := p.Restart("mon-api-staging"); err != nil {
		t.Fatal(err)
	}
	if v, _ := parseHelmValues(scm.files[dir+"values.yaml"]); v.ReplicaCount != 1 || v.PodAnnotations[restartAnnotation] == "" {
		t.Errorf("après Restart : %+v", v)
	}
	if app := argo.apps["mon-api-staging"]; app.Spec.Source.Helm == nil {
		t.Error("Restart a perdu la source Helm")
	}

	// Un chart sans Chart.yaml est refusé
	if _, err := p.Deploy(stackRequest("helm", map[string]string{"values.yaml": "x"})); err == nil {
		t.Error("chart sans Chart.yaml accepté")
	}
}

func TestDeployKustomize(t *testing.T) {
	p, scm, argo := newSecretTest(t)
	if _, err := p.Deploy(stackRequest("kustomize", testBase)); err != nil {
		t.Fatal(err)
	}
	dir := "apps/mon-api-staging/"
	for _, f := range []string{"base/kustomization.yaml", "base/deployment.yaml", "kustomization.yaml", "ingress.yaml", secretFile} {
		if _, ok := scm.files[dir+f]; !ok {
			t.Errorf("%s absent ; fichiers : %v", f, keys(scm.files))
		}
	}
	k, err := parseKustomization(scm.files[dir+"kustomization.yaml"])
	if err != nil {
		t.Fatal(err)
	}
	if k.NamePrefix != "mon-api-staging-" || k.Images[0].Name != "app" || k.Images[0].NewName != "localhost:5050/root/mon-api" ||
		len(k.ConfigMapGenerator) != 1 || k.ConfigMapGenerator[0].Literals[1] != "B=deux: points" ||
		len(k.Patches) != 1 || !strings.Contains(k.Patches[0].Patch, "mon-api-staging-env") {
		t.Errorf("kustomization = %+v", k)
	}
	if !strings.Contains(scm.files[dir+"ingress.yaml"], "name: app") {
		t.Errorf("l'Ingress doit viser le Service app de la base :\n%s", scm.files[dir+"ingress.yaml"])
	}
	if app := argo.apps["mon-api-staging"]; app.Spec.Source.Path != "apps/mon-api-staging" || app.Spec.Source.Helm != nil {
		t.Errorf("source de l'Application : %+v", app.Spec.Source)
	}
	// Stop garde le préfixe, les variables et le patch
	if err := p.Stop("mon-api-staging"); err != nil {
		t.Fatal(err)
	}
	k2, _ := parseKustomization(scm.files[dir+"kustomization.yaml"])
	if k2.Replicas[0].Count != 0 || k2.NamePrefix != k.NamePrefix || len(k2.Patches) != 1 || len(k2.ConfigMapGenerator) != 1 || len(k2.Labels) != 1 {
		t.Errorf("après Stop : %+v", k2)
	}
	if _, err := p.Deploy(stackRequest("kustomize", map[string]string{"deployment.yaml": "x"})); err == nil {
		t.Error("base sans kustomization.yaml acceptée")
	}
	if _, err := p.Deploy(stackRequest("kustomize", map[string]string{"kustomization.yaml": "x", "../../evil.yaml": "x"})); err == nil {
		t.Error("chemin hors du dossier accepté")
	}
}

// Logs : le Deployment d'une stack peut porter un autre nom (base Kustomize)
func TestLogsStackDeployment(t *testing.T) {
	p, _, argo := newSecretTest(t)
	if _, err := p.Deploy(stackRequest("kustomize", testBase)); err != nil {
		t.Fatal(err)
	}
	app := argo.apps["mon-api-staging"]
	app.Status = &appStatus{}
	app.Status.Resources = append(app.Status.Resources, struct {
		Kind string `json:"kind"`
		Name string `json:"name"`
	}{"Deployment", "mon-api-staging-app"})
	argo.apps["mon-api-staging"] = app
	if _, err := p.Logs("mon-api-staging", 10); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(argo.lastLogs, "resourceName=mon-api-staging-app") {
		t.Errorf("journaux demandés : %s", argo.lastLogs)
	}
}

// Les fichiers générés passent par kustomize et helm, s'ils sont installés
// (la CI ne les a pas : le test est alors sauté)
func TestStackManifestsRender(t *testing.T) {
	p, scm, _ := newSecretTest(t)
	for kind, files := range map[string]map[string]string{"kustomize": testBase, "helm": testChart} {
		bin := kind
		if _, err := exec.LookPath(bin); err != nil {
			t.Logf("%s absent : rendu non vérifié", bin)
			continue
		}
		scm.files = map[string]string{}
		if _, err := p.Deploy(stackRequest(kind, files)); err != nil {
			t.Fatal(err)
		}
		dir := t.TempDir()
		for f, c := range scm.files {
			path := filepath.Join(dir, strings.TrimPrefix(f, "apps/mon-api-staging/"))
			os.MkdirAll(filepath.Dir(path), 0o755)
			os.WriteFile(path, []byte(c), 0o644)
		}
		var cmd *exec.Cmd
		if kind == "kustomize" {
			cmd = exec.Command("kustomize", "build", dir)
		} else {
			cmd = exec.Command("helm", "template", "x", filepath.Join(dir, "chart"), "-f", filepath.Join(dir, "values.yaml"))
		}
		out, err := cmd.CombinedOutput()
		if err != nil {
			t.Fatalf("%s : %v\n%s", kind, err, out)
		}
		for _, want := range []string{"localhost:5050/root/mon-api:abc123", "mon-api-staging", "kind: ExternalSecret", "name: mon-api-staging-env"} {
			if !strings.Contains(string(out), want) {
				t.Errorf("%s : %q absent du rendu :\n%s", kind, want, out)
			}
		}
	}
}

func keys(m map[string]string) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	return out
}
