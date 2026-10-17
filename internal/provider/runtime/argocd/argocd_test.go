package argocd

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
	"gopkg.in/yaml.v3"
)

// fakeSCM : repo de déploiement en mémoire, un commit par appel de PushFiles
type fakeSCM struct {
	provider.SCMProvider
	mu      sync.Mutex
	files   map[string]string
	commits []string
}

func (f *fakeSCM) PushFiles(repo, branch string, files map[string]string, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	for p, c := range files {
		f.files[p] = c
	}
	f.commits = append(f.commits, msg)
	return nil
}

func (f *fakeSCM) GetFileContent(repo, branch, path string) (string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.files[path]
	if !ok {
		return "", provider.ErrNotFound
	}
	return c, nil
}

func (f *fakeSCM) ListFiles(repo, branch, dir string) ([]string, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []string
	for p := range f.files {
		if strings.HasPrefix(p, dir+"/") {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, provider.ErrNotFound
	}
	return out, nil
}

func (f *fakeSCM) DeleteFile(repo, branch, path, msg string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if _, ok := f.files[path]; !ok {
		return provider.ErrNotFound
	}
	delete(f.files, path)
	f.commits = append(f.commits, msg)
	return nil
}

// fakeArgo : API Argo CD minimale (Applications en mémoire, journaux fixes)
type fakeArgo struct {
	mu       sync.Mutex
	apps     map[string]application
	refresh  []string
	token    string
	lastLogs string // requête de journaux reçue
}

func (a *fakeArgo) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Header.Get("Authorization") != "Bearer "+a.token {
		http.Error(w, `{"message":"invalid session"}`, http.StatusUnauthorized)
		return
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	name := strings.TrimPrefix(r.URL.Path, "/api/v1/applications/")
	switch {
	case r.URL.Path == "/api/v1/session/userinfo":
		io.WriteString(w, `{"loggedIn":true,"username":"symphony"}`)
	case r.URL.Path == "/api/version":
		io.WriteString(w, `{"Version":"v3.1.0"}`)
	case r.Method == "POST" && r.URL.Path == "/api/v1/applications" && r.URL.Query().Get("upsert") == "true":
		var app application
		json.NewDecoder(r.Body).Decode(&app)
		if prev, ok := a.apps[app.Metadata.Name]; ok {
			app.Status = prev.Status
			// Comme Argo CD : les labels existants sont fusionnés, pas remplacés
			for k, v := range prev.Metadata.Labels {
				if _, set := app.Metadata.Labels[k]; !set {
					app.Metadata.Labels[k] = v
				}
			}
		}
		a.apps[app.Metadata.Name] = app
		json.NewEncoder(w).Encode(app)
	case r.Method == "GET" && r.URL.Path == "/api/v1/applications":
		// Sélecteur de labels : k=v[,k=v…], toujours sur les Applications de Symphony
		selector := r.URL.Query().Get("selector")
		if !strings.HasPrefix(selector, "app.kubernetes.io/managed-by=symphony") {
			http.Error(w, "selector", http.StatusBadRequest)
			return
		}
		items := []application{}
	apps:
		for _, app := range a.apps {
			for _, kv := range strings.Split(selector, ",") {
				k, v, _ := strings.Cut(kv, "=")
				if app.Metadata.Labels[k] != v {
					continue apps
				}
			}
			items = append(items, app)
		}
		json.NewEncoder(w).Encode(map[string]any{"items": items})
	case r.Method == "GET" && strings.HasSuffix(name, "/logs"):
		a.lastLogs = r.URL.RawQuery
		io.WriteString(w, `{"result":{"content":"démarrage","podName":"p"}}`+"\n"+
			`{"result":{"content":"prêt","podName":"p"}}`+"\n"+
			`{"result":{"content":"","last":true}}`+"\n")
	case r.Method == "DELETE":
		if _, ok := a.apps[name]; !ok {
			http.Error(w, `{"message":"permission denied"}`, http.StatusForbidden)
			return
		}
		if r.URL.Query().Get("cascade") != "true" {
			http.Error(w, "cascade attendu", http.StatusBadRequest)
			return
		}
		delete(a.apps, name)
		io.WriteString(w, "{}")
	case r.Method == "GET":
		if r.URL.Query().Get("refresh") != "" {
			a.refresh = append(a.refresh, name)
		}
		app, ok := a.apps[name]
		if !ok {
			http.Error(w, `{"message":"permission denied"}`, http.StatusForbidden)
			return
		}
		json.NewEncoder(w).Encode(app)
	default:
		http.Error(w, "route inconnue", http.StatusNotFound)
	}
}

func newTest(t *testing.T) (*Provider, *fakeSCM, *fakeArgo) {
	t.Helper()
	argo := &fakeArgo{apps: map[string]application{}, token: "jeton"}
	srv := httptest.NewServer(argo)
	t.Cleanup(srv.Close)
	scm := &fakeSCM{files: map[string]string{}}
	p, err := New("argocd", scm, Config{
		URL:           srv.URL,
		Token:         "jeton",
		Repo:          "root/symphony-deployments",
		Path:          "apps",
		RepoURL:       "http://gitlab/root/symphony-deployments.git",
		Namespace:     "symphony-apps",
		IngressDomain: "localhost",
		IngressPort:   "8081",
	})
	if err != nil {
		t.Fatal(err)
	}
	return p, scm, argo
}

func TestDeploy(t *testing.T) {
	p, scm, argo := newTest(t)
	d, err := p.Deploy(provider.DeployRequest{
		Name:         "mon-api",
		Image:        "localhost:5050/root/mon-api",
		Tag:          "abc123",
		Port:         3000,
		EnvVars:      map[string]string{"B": "deux: points", "A": "un"},
		RegistryAuth: &provider.RegistryAuth{Password: "secret"},
	})
	if err != nil {
		t.Fatal(err)
	}

	// Un commit, quatre fichiers YAML valides, aucun secret
	if len(scm.commits) != 1 || !strings.Contains(scm.commits[0], "localhost:5050/root/mon-api:abc123") {
		t.Errorf("commits = %q", scm.commits)
	}
	for _, f := range []string{"kustomization.yaml", "deployment.yaml", "service.yaml", "ingress.yaml"} {
		content, ok := scm.files["apps/mon-api/"+f]
		if !ok {
			t.Fatalf("%s absent ; fichiers : %v", f, scm.files)
		}
		var doc map[string]any
		if err := yaml.Unmarshal([]byte(content), &doc); err != nil {
			t.Errorf("%s : YAML invalide : %v\n%s", f, err, content)
		}
		if strings.Contains(content, "secret") {
			t.Errorf("%s contient les identifiants du registry", f)
		}
	}
	dep := scm.files["apps/mon-api/deployment.yaml"]
	for _, want := range []string{`image: "localhost:5050/root/mon-api"`, "containerPort: 3000",
		`- name: "A"`, `value: "deux: points"`} {
		if !strings.Contains(dep, want) {
			t.Errorf("deployment.yaml sans %q :\n%s", want, dep)
		}
	}
	if strings.Index(dep, `"A"`) > strings.Index(dep, `"B"`) {
		t.Error("variables d'environnement non triées (diff instable d'un déploiement à l'autre)")
	}
	k, err := parseKustomization(scm.files["apps/mon-api/kustomization.yaml"])
	if err != nil {
		t.Fatal(err)
	}
	if k.Images[0] != (kustomizeImage{Name: "localhost:5050/root/mon-api", NewTag: "abc123"}) || k.Replicas[0].Count != 1 {
		t.Errorf("kustomization = %+v", k)
	}
	if !strings.Contains(scm.files["apps/mon-api/ingress.yaml"], `host: "mon-api.localhost"`) {
		t.Errorf("ingress.yaml :\n%s", scm.files["apps/mon-api/ingress.yaml"])
	}

	// Application : source = dossier du service, synchro automatique
	app := argo.apps["mon-api"]
	if app.Spec.Source.Path != "apps/mon-api" || app.Spec.Source.RepoURL != "http://gitlab/root/symphony-deployments.git" ||
		app.Spec.Source.TargetRevision != "main" || app.Spec.Destination.Namespace != "symphony-apps" ||
		!app.Spec.SyncPolicy.Automated.SelfHeal || app.Metadata.Labels[managedLabel] != managedValue {
		t.Errorf("application = %+v", app)
	}
	if len(argo.refresh) != 1 {
		t.Errorf("refresh = %v, attendu un rafraîchissement après le commit", argo.refresh)
	}
	if d.ID != "mon-api" || d.URL != "http://mon-api.localhost:8081" || d.Status != "deploying" {
		t.Errorf("déploiement = %+v", d)
	}
}

func TestDeployNomInvalide(t *testing.T) {
	p, scm, _ := newTest(t)
	if _, err := p.Deploy(provider.DeployRequest{Name: "Mon_API", Image: "x"}); err == nil {
		t.Fatal("nom invalide accepté")
	}
	if len(scm.commits) != 0 {
		t.Error("commit malgré un nom invalide")
	}
}

func TestStopRestart(t *testing.T) {
	p, scm, argo := newTest(t)
	if _, err := p.Deploy(provider.DeployRequest{Name: "svc", Image: "nginx:1.27"}); err != nil {
		t.Fatal(err)
	}
	kfile := "apps/svc/kustomization.yaml"

	if err := p.Stop("svc"); err != nil {
		t.Fatal(err)
	}
	k, _ := parseKustomization(scm.files[kfile])
	if k.Replicas[0].Count != 0 || argo.apps["svc"].Metadata.Labels[stoppedLabel] != "true" {
		t.Errorf("après Stop : réplicas %d, labels %v", k.Replicas[0].Count, argo.apps["svc"].Metadata.Labels)
	}
	if d, _ := p.Status("svc"); d.Status != "stopped" {
		t.Errorf("statut après Stop = %s", d.Status)
	}

	if err := p.Restart("svc"); err != nil {
		t.Fatal(err)
	}
	k, _ = parseKustomization(scm.files[kfile])
	if k.Replicas[0].Count != 1 || k.CommonAnnotations[restartAnnotation] == "" || k.Images[0].NewTag != "1.27" {
		t.Errorf("après Restart : %+v", k)
	}
	if d, _ := p.Status("svc"); d.Status == "stopped" {
		t.Error("toujours marqué arrêté après Restart")
	}
	if len(scm.commits) != 3 {
		t.Errorf("commits = %q, attendu deploy, stop, restart", scm.commits)
	}

	if err := p.Stop("inconnu"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("Stop d'un service inconnu : %v", err)
	}
}

func TestStatusListLogs(t *testing.T) {
	p, _, argo := newTest(t)
	if _, err := p.Status("absent"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("Status absent : %v", err)
	}
	if _, err := p.Deploy(provider.DeployRequest{Name: "svc", Image: "nginx"}); err != nil {
		t.Fatal(err)
	}
	app := argo.apps["svc"]
	app.Status = &appStatus{}
	app.Status.Health.Status = "Healthy"
	app.Status.Sync.Status = "Synced"
	app.Status.Summary.Images = []string{"nginx:latest"}
	argo.apps["svc"] = app

	list, err := p.List()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 1 || list[0].Status != "running" || list[0].Sync != "Synced" || list[0].Image != "nginx:latest" {
		t.Errorf("List = %+v", list)
	}

	logs, err := p.Logs("svc", 50)
	if err != nil {
		t.Fatal(err)
	}
	if logs != "démarrage\nprêt\n" {
		t.Errorf("Logs = %q", logs)
	}
	for _, want := range []string{"kind=Deployment", "resourceName=svc", "namespace=symphony-apps", "tailLines=50", "follow=false"} {
		if !strings.Contains(argo.lastLogs, want) {
			t.Errorf("requête de journaux sans %s : %s", want, argo.lastLogs)
		}
	}
}

func TestJetonRefuse(t *testing.T) {
	p, _, _ := newTest(t)
	p.cfg.Token = "mauvais"
	_, err := p.List()
	if err == nil || !strings.Contains(err.Error(), "invalid session") {
		t.Errorf("erreur attendue avec le message d'Argo CD, obtenu %v", err)
	}
}

func TestDeploymentStatus(t *testing.T) {
	cases := map[string]string{"Healthy": "running", "Progressing": "deploying", "Missing": "deploying",
		"": "deploying", "Degraded": "error", "Unknown": "error", "Suspended": "stopped"}
	for health, want := range cases {
		if got := deploymentStatus(health, false); got != want {
			t.Errorf("deploymentStatus(%q) = %s, attendu %s", health, got, want)
		}
	}
	if deploymentStatus("Healthy", true) != "stopped" {
		t.Error("un service arrêté doit être stopped même Healthy")
	}
}

func TestRemove(t *testing.T) {
	p, scm, argo := newTest(t)
	// svc : sans environnement (ancien) et en staging et production ; svc-bis :
	// un autre projet
	for _, req := range []provider.DeployRequest{
		{Name: "svc", Image: "nginx:1.27"},
		{Name: "svc-staging", Project: "svc", Environment: "staging", Image: "nginx:1.27"},
		{Name: "svc-production", Project: "svc", Environment: "production", Image: "nginx:1.27"},
		{Name: "svc-bis", Image: "nginx:1.27"},
	} {
		if _, err := p.Deploy(req); err != nil {
			t.Fatal(err)
		}
	}
	if d, _ := p.Status("svc-production"); d.Project != "svc" || d.Environment != "production" {
		t.Errorf("projet %q, environnement %q", d.Project, d.Environment)
	}
	var autre application
	autre.Metadata.Name = "autre"
	argo.apps["autre"] = autre

	if err := p.Remove("svc"); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"svc", "svc-staging", "svc-production"} {
		if _, err := p.Status(name); !errors.Is(err, provider.ErrNotFound) {
			t.Errorf("Application %s toujours là : %v", name, err)
		}
	}
	for f := range scm.files {
		if strings.HasPrefix(f, "apps/svc/") || strings.HasPrefix(f, "apps/svc-staging/") || strings.HasPrefix(f, "apps/svc-production/") {
			t.Errorf("manifest restant : %s", f)
		}
	}
	if _, ok := scm.files["apps/svc-bis/kustomization.yaml"]; !ok {
		t.Error("les manifests de svc-bis ont été supprimés")
	}
	if _, ok := argo.apps["svc-bis"]; !ok {
		t.Error("l'Application svc-bis a été supprimée")
	}

	// Rejouée : rien à faire, pas d'erreur
	if err := p.Remove("svc"); err != nil {
		t.Errorf("Remove rejoué : %v", err)
	}
	// Application que Symphony n'a pas créée : laissée en place
	if err := p.Remove("autre"); err == nil {
		t.Error("Remove d'une Application non gérée : pas d'erreur")
	}
	if _, ok := argo.apps["autre"]; !ok {
		t.Error("Application non gérée supprimée")
	}
}

// TestStopKeepsOwner : arrêter réécrit l'Application sans perdre son projet
// ni son environnement
func TestStopKeepsOwner(t *testing.T) {
	p, _, argo := newTest(t)
	if _, err := p.Deploy(provider.DeployRequest{Name: "api-production", Project: "api", Environment: "production", Image: "nginx:1.27"}); err != nil {
		t.Fatal(err)
	}
	if err := p.Stop("api-production"); err != nil {
		t.Fatal(err)
	}
	labels := argo.apps["api-production"].Metadata.Labels
	if labels[projectLabel] != "api" || labels[envLabel] != "production" || labels[stoppedLabel] != "true" {
		t.Errorf("labels après arrêt : %v", labels)
	}
}

// TestDeploySecretRefs : les secrets d'une application vont dans une
// ExternalSecret (références), jamais leur valeur dans Git
func TestDeploySecretRefs(t *testing.T) {
	p, scm, _ := newTest(t)
	refs := map[string]provider.SecretRef{"DB_PASSWORD": {Provider: "vault", Path: "apps/api/db", Key: "password"}}
	if _, err := p.Deploy(provider.DeployRequest{Name: "api", Image: "nginx:1.27", SecretEnv: refs}); err == nil {
		t.Error("secrets acceptés sans secret_store")
	}
	p.cfg.SecretStore, p.cfg.SecretStoreProvider = "symphony-vault", "vault"
	if !p.SecretRefs() {
		t.Error("SecretRefs = false avec un secret_store")
	}
	if _, err := p.Deploy(provider.DeployRequest{Name: "api", Image: "nginx:1.27", EnvVars: map[string]string{"LOG": "info"}, SecretEnv: refs}); err != nil {
		t.Fatal(err)
	}
	es := scm.files["apps/api/externalsecret.yaml"]
	for _, want := range []string{"kind: ExternalSecret", `name: "symphony-vault"`, `key: "apps/api/db"`, `property: "password"`, "name: api-env"} {
		if !strings.Contains(es, want) {
			t.Errorf("ExternalSecret sans %q :\n%s", want, es)
		}
	}
	dep := scm.files["apps/api/deployment.yaml"]
	if !strings.Contains(dep, "secretKeyRef:") || !strings.Contains(dep, `value: "info"`) {
		t.Errorf("deployment.yaml :\n%s", dep)
	}
	if !strings.Contains(scm.files["apps/api/kustomization.yaml"], "externalsecret.yaml") {
		t.Error("ExternalSecret absente de kustomization.yaml")
	}
	// Un secret d'un autre coffre n'est pas lu par ce store
	refs["X"] = provider.SecretRef{Provider: "autre", Path: "apps/api/x"}
	if _, err := p.Deploy(provider.DeployRequest{Name: "api", Image: "nginx:1.27", SecretEnv: refs}); err == nil {
		t.Error("secret d'un autre provider accepté")
	}
}

func TestCheck(t *testing.T) {
	p, _, _ := newTest(t)
	if detail, err := p.Check(); err != nil || detail != "Argo CD v3.1.0, connecté en tant que symphony" {
		t.Errorf("Check = %q, %v", detail, err)
	}
	p.cfg.Token = "mauvais"
	if _, err := p.Check(); err == nil || !strings.Contains(err.Error(), "jeton refusé") {
		t.Errorf("jeton refusé attendu, obtenu %v", err)
	}
}
