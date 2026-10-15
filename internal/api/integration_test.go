package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/api/handlers"
	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/database/dbtest"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/fake"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/clemuscle/symphony/internal/webhook"
)

// vault : coffre de secrets en mémoire, au format des références secret://
type vault map[string]string

func (v vault) ID() string   { return "vault" }
func (v vault) Type() string { return "fake" }
func (v vault) Get(path, key string) (string, error) {
	if s, ok := v[path+"#"+key]; ok {
		return s, nil
	}
	return "", provider.ErrNotFound
}

// golden path minimal, poussé dans le repo de templates de la forge
var templateFiles = map[string]string{
	"templates/api-go/template.yaml": `apiVersion: symphony/v1
kind: GoldenPath
metadata:
  name: api-go
  title: API Go
spec:
  language: go
  type: api
  providers: {scm: gitlab, ci: gitlab-ci, registry: registry, runtime: docker-local}
  skeleton: skeleton
  ci_template: ci.yml
`,
	"templates/api-go/ci.yml":           "build: {script: [make]}\n",
	"templates/api-go/skeleton/main.go": "package main // {{SERVICE_NAME}}\n",
}

// TestIntegration : le parcours complet par l'API, avec les tâches, sur une
// vraie base et des providers simulés : création (repo, squelette, première
// pipeline), fiche avec variables et secrets, déploiement en staging (secrets
// résolus), refus d'un déploiement direct en production, promotion (secrets en
// références), suppression de tout.
func TestIntegration(t *testing.T) {
	db := dbtest.Open(t)

	registry := fake.NewRegistry()
	forge := fake.NewForge(registry)
	docker := fake.NewRuntime("docker-local", false)
	k8s := fake.NewRuntime("argocd-k3s", true)
	providers := provider.NewRegistry()
	providers.RegisterSCM(forge.SCM(), true)
	providers.RegisterCI(forge.CI(), true)
	providers.RegisterRegistry(registry, true)
	providers.RegisterRuntime(docker, true)
	providers.RegisterRuntime(k8s, false)

	if _, err := forge.SCM().CreateRepo(provider.CreateRepoRequest{Name: "symphony-templates", Namespace: "platform"}); err != nil {
		t.Fatal(err)
	}
	if err := forge.SCM().PushFiles("platform/symphony-templates", "main", templateFiles, "templates [skip ci]"); err != nil {
		t.Fatal(err)
	}
	templates := gitops.NewTemplateLoader(providers, "platform/symphony-templates", "main", "templates")
	templates.Reload()
	if len(templates.GetGoldenPaths()) != 1 {
		t.Fatalf("golden paths : %+v", templates.GetGoldenPaths())
	}

	name := fmt.Sprintf("it-%d", time.Now().UnixNano()%1e9)
	providers.RegisterSecrets(vault{"apps/" + name + "/db#password": "s3cr3t-db"}, true)

	store := catalogue.NewStore()
	syncer := gitops.NewSyncer(providers, store, func() ([]gitops.ProjectRef, error) {
		projects, err := db.ListProjects()
		refs := make([]gitops.ProjectRef, 0, len(projects))
		for _, p := range projects {
			refs = append(refs, gitops.ProjectRef{Name: p.Name, RepoPath: p.RepoPath, Branch: "main", SCMProvider: p.SCMProvider})
		}
		return refs, err
	}, "")
	envs := environments.NewHolder([]environments.Environment{
		{Name: "staging", Runtime: "docker-local"},
		{Name: "production", Runtime: "argocd-k3s", Protected: true},
	})
	runner := tasks.NewRunner(db, providers)
	runner.PollInterval = 100 * time.Millisecond
	hooks := webhook.NewHolder(nil)
	handlers.RegisterTasks(runner, handlers.TaskDeps{
		Providers: providers, DB: db, Templates: templates, Syncer: syncer, Webhooks: hooks, Store: store, Envs: envs,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx, 2)

	srv := httptest.NewServer(NewServer(providers, store, db, syncer, templates,
		auth.NewHolder(&auth.Policy{}), runner, hooks, envs))
	defer srv.Close()

	call := func(method, path string, body any) (int, map[string]any) {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			json.NewEncoder(&buf).Encode(body)
		}
		req, _ := http.NewRequest(method, srv.URL+path, &buf)
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", srv.URL) // même origine : authentification désactivée
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		var out map[string]any
		json.NewDecoder(resp.Body).Decode(&out)
		return resp.StatusCode, out
	}
	// wait : attend la fin de la tâche lancée (202)
	wait := func(step string, code int, task map[string]any) {
		t.Helper()
		if code != http.StatusAccepted {
			t.Fatalf("%s : %d %v", step, code, task)
		}
		id := int64(task["id"].(float64))
		deadline := time.Now().Add(30 * time.Second)
		for time.Now().Before(deadline) {
			got, err := db.GetTask(id)
			if err != nil {
				t.Fatal(err)
			}
			switch got.Status {
			case database.TaskSuccess:
				return
			case database.TaskFailed:
				t.Fatalf("%s : tâche en échec : %s", step, got.Error)
			}
			time.Sleep(50 * time.Millisecond)
		}
		t.Fatalf("%s : tâche toujours en cours", step)
	}

	// 1. Création : repo, squelette, fiche et CI en un commit, première pipeline
	code, task := call("POST", "/api/v1/projects", map[string]any{"name": name, "golden_path_id": "api-go", "owner": "team-it"})
	wait("création", code, task)
	repo := "apps/" + name
	if _, ok := forge.File(repo, "catalog-info.yaml"); !ok {
		t.Fatalf("fiche absente du repo %s", repo)
	}
	if c, _ := forge.File(repo, "main.go"); !strings.Contains(c, name) {
		t.Errorf("placeholders non remplacés : %q", c)
	}

	// 2. La fiche déclare les variables par environnement, dont un secret
	fiche := `apiVersion: symphony/v1
kind: Service
metadata: {name: ` + name + `, owner: team-it}
spec:
  repo: http://forge/apps/` + name + `
  actions:
    - name: Rollback
      ci_provider: gitlab-ci
      inputs:
        - {id: version, type: string, required: true}
  environments:
    - name: staging
      env: {LOG_LEVEL: debug, DB_PASSWORD: "secret://vault/apps/` + name + `/db#password"}
    - name: production
      env: {LOG_LEVEL: info, DB_PASSWORD: "secret://vault/apps/` + name + `/db#password"}
`
	forge.SCM().PushFiles(repo, "main", map[string]string{"catalog-info.yaml": fiche}, "fiche [skip ci]")
	syncer.ForceSync()

	// 3. Image publiée par la pipeline
	code, img := call("GET", "/api/v1/projects/"+name+"/image", nil)
	tags, _ := img["tags"].([]any)
	if code != http.StatusOK || len(tags) == 0 {
		t.Fatalf("image : %d %v", code, img)
	}
	image, tag := img["image"].(string), tags[0].(string)

	// 4. Production protégée : pas de déploiement direct
	code, out := call("POST", "/api/v1/deployments", map[string]any{"project_name": name, "environment": "production", "image": image, "tag": tag})
	if code != http.StatusForbidden || !strings.Contains(out["error"].(string), "protégé") {
		t.Errorf("déploiement direct en production : %d %v", code, out)
	}

	// 5. Staging (Docker) : secret résolu par Symphony
	code, task = call("POST", "/api/v1/deployments", map[string]any{"project_name": name, "environment": "staging", "image": image, "tag": tag})
	wait("staging", code, task)
	staging := provider.DeploymentName(name, "staging")
	if env := docker.LastEnv[staging]; env["DB_PASSWORD"] != "s3cr3t-db" || env["LOG_LEVEL"] != "debug" {
		t.Errorf("variables en staging : %v", env)
	}

	// 6. Promotion en production (Argo CD) : la même image, secrets en références
	code, task = call("POST", "/api/v1/projects/"+name+"/promote", map[string]any{"to": "production"})
	wait("promotion", code, task)
	production := provider.DeploymentName(name, "production")
	d, err := k8s.Status(production)
	if err != nil || d.Image != image+":"+tag {
		t.Fatalf("production : %+v %v", d, err)
	}
	if env := k8s.LastEnv[production]; env["DB_PASSWORD"] != "" || env["LOG_LEVEL"] != "info" {
		t.Errorf("valeur d'un secret transmise au runtime GitOps : %v", env)
	}
	if ref := k8s.LastSecretEnv[production]["DB_PASSWORD"]; ref.Path != "apps/"+name+"/db" {
		t.Errorf("référence du secret : %+v", ref)
	}
	// Aucune valeur de secret dans les tâches (renvoyées par l'API)
	list, _ := db.ListTasks(database.TaskFilter{Resource: name, Limit: 50})
	for _, tk := range list {
		if bytes.Contains(tk.Result, []byte("s3cr3t-db")) || bytes.Contains(tk.Params, []byte("s3cr3t-db")) {
			t.Errorf("secret dans la tâche #%d", tk.ID)
		}
	}

	// Lectures : le projet et ses environnements, les déploiements par
	// environnement, relance, journaux
	code, _ = callList(t, srv.URL, "/api/v1/projects", func(items []map[string]any) bool {
		for _, p := range items {
			if p["name"] == name {
				envs, _ := p["environments"].([]any)
				return p["team"] == "team-it" && len(envs) == 2
			}
		}
		return false
	})
	code, _ = callList(t, srv.URL, "/api/v1/deployments", func(items []map[string]any) bool {
		n := 0
		for _, d := range items {
			if d["project"] == name {
				n++
			}
		}
		return n == 2
	})
	if code, out := call("POST", "/api/v1/deployments/"+staging+"/restart?provider=docker-local", nil); code != http.StatusOK {
		t.Errorf("relance : %d %v", code, out)
	}
	if code, out := call("GET", "/api/v1/deployments/"+production+"/logs?provider=argocd-k3s", nil); code != http.StatusOK || !strings.Contains(fmt.Sprint(out["logs"]), "prêt") {
		t.Errorf("journaux : %d %v", code, out)
	}

	// Action Day-2 (pipeline) : entrées validées, puis tâche suivie jusqu'à la
	// fin de sa pipeline
	if code, out := call("POST", "/api/v1/services/"+name+"/actions/Rollback", map[string]any{"version": "v1", "pirate": "x"}); code != http.StatusBadRequest {
		t.Errorf("entrée non déclarée acceptée : %d %v", code, out)
	}
	code, task = call("POST", "/api/v1/services/"+name+"/actions/Rollback", map[string]any{"version": "v1"})
	wait("action Day-2", code, task)
	code, _ = callList(t, srv.URL, "/api/v1/tasks?resource="+name, func(items []map[string]any) bool { return len(items) >= 4 })
	code, _ = callList(t, srv.URL, "/api/v1/audit", func(items []map[string]any) bool {
		for _, e := range items {
			if e["action"] == "deploy" && e["resource"] == name {
				return true
			}
		}
		return false
	})

	// 7. Suppression : tous les environnements, le repo, les images
	code, task = call("DELETE", "/api/v1/projects/"+name, nil)
	wait("suppression", code, task)
	for _, rt := range []*fake.Runtime{docker, k8s} {
		if deps, _ := rt.List(); len(deps) != 0 {
			t.Errorf("déploiements restants sur %s : %v", rt.ID(), deps)
		}
	}
	for _, r := range forge.Repos() {
		if r == repo {
			t.Error("repo toujours là")
		}
	}
	if tags, _ := registry.ListTags(repo); len(tags) != 0 {
		t.Errorf("images restantes : %v", tags)
	}
	if _, err := db.GetProjectByName(name); err == nil {
		t.Error("projet toujours en base")
	}
}

// callList : GET d'une liste, vérifiée par ok
func callList(t *testing.T, base, path string, ok func([]map[string]any) bool) (int, []map[string]any) {
	t.Helper()
	resp, err := http.Get(base + path)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	var items []map[string]any
	json.NewDecoder(resp.Body).Decode(&items)
	if resp.StatusCode != http.StatusOK || !ok(items) {
		t.Errorf("GET %s : %d, liste inattendue (%d éléments)", path, resp.StatusCode, len(items))
	}
	return resp.StatusCode, items
}
