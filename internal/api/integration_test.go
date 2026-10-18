package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/api/handlers"
	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/config"
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

func (v vault) ID() string             { return "vault" }
func (v vault) Type() string           { return "fake" }
func (v vault) Check() (string, error) { return "", nil }
func (v vault) Get(path, key string) (string, error) {
	if s, ok := v[path+"#"+key]; ok {
		return s, nil
	}
	return "", provider.ErrNotFound
}

// stack minimale avec des paramètres, poussée dans le repo de stacks de la forge
var stackFiles = map[string]string{
	"stacks/api-go/stack.yaml": `apiVersion: symphony/v1
kind: Stack
metadata:
  name: api-go
  title: API Go
spec:
  language: go
  type: api
  providers: {scm: gitlab, ci: gitlab-ci, registry: registry, runtime: docker-local}
  skeleton: skeleton
  ci_template: ci.yml
  parameters:
    - {name: log_level, type: choice, choices: [info, debug], default: info}
    - {name: metrics, type: boolean, default: true}
    - {name: metrics_path, type: string, default: /metrics, when: {metrics: true}}
  files:
    - {path: metrics.go, when: {metrics: true}}
`,
	"stacks/api-db/stack.yaml": `apiVersion: symphony/v1
kind: Stack
metadata:
  name: api-db
spec:
  providers: {scm: gitlab, ci: gitlab-ci, registry: registry, runtime: argocd-k3s}
  ci_template: ci.yml
  environments: {staging: argocd-k3s}
  parameters:
    - {name: postgres, type: boolean, default: true}
  files:
    - {path: deploy/infra, when: {postgres: true}}
  infra:
    kubernetes: deploy/infra
    env: {DATABASE_URL: {secret: db-app, key: uri}}
`,
	"stacks/api-db/ci.yml":                        "build: {script: [make]}\n",
	"stacks/api-db/skeleton/main.go":              "package main\n",
	"stacks/api-db/skeleton/deploy/infra/db.yaml": "apiVersion: postgresql.cnpg.io/v1\nkind: Cluster\nmetadata: {name: db}\n",
	"stacks/api-go/ci.yml":                        "build: {script: [make]}\n",
	"stacks/api-go/skeleton/main.go.tmpl":         "package main // {{SERVICE_NAME}} {{params.log_level}}\n",
	"stacks/api-go/skeleton/metrics.go.tmpl":      "package main // {{params.metrics_path}}\n",
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
	k8s.Kind = "argocd"
	providers := provider.NewRegistry()
	providers.RegisterSCM(forge.SCM(), true)
	providers.RegisterCI(forge.CI(), true)
	providers.RegisterRegistry(registry, true)
	providers.RegisterRuntime(docker, true)
	providers.RegisterRuntime(k8s, false)

	if _, err := forge.SCM().CreateRepo(provider.CreateRepoRequest{Name: "symphony-stacks", Namespace: "platform"}); err != nil {
		t.Fatal(err)
	}
	if err := forge.SCM().PushFiles("platform/symphony-stacks", "main", stackFiles, "stacks [skip ci]"); err != nil {
		t.Fatal(err)
	}
	stacks := gitops.NewStackLoader(providers, nil, gitops.StackSource{Repo: "platform/symphony-stacks", Branch: "main", Path: "stacks"})
	stacks.Reload()
	if len(stacks.GetStacks()) != 2 {
		t.Fatalf("stacks : %+v, %+v", stacks.GetStacks(), stacks.Report())
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
		Providers: providers, DB: db, Stacks: stacks, Syncer: syncer, Webhooks: hooks, Store: store, Envs: envs,
	})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.Start(ctx, 2)

	discovery := gitops.NewDiscoverer(providers, handlers.DiscoveryStore{DB: db, Providers: providers}, syncer.File, syncer.ForceSync)
	srv := httptest.NewServer(NewServer(providers, store, db, syncer, discovery, stacks,
		auth.NewHolder(&auth.Policy{}), runner, hooks, envs, func() config.Status { return config.Status{} }))
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

	// 1. Création : repo, squelette, fiche et CI en un commit, première
	// pipeline ; les paramètres sont validés avant (JSON Schema de la stack)
	if code, body := call("POST", "/api/v1/projects", map[string]any{"name": name, "stack": "api-go", "owner": "team-it",
		"parameters": map[string]any{"log_level": "trace"}}); code != http.StatusBadRequest ||
		!strings.Contains(fmt.Sprint(body["error"]), "log_level") {
		t.Errorf("paramètre invalide : %d %v", code, body)
	}
	code, task := call("POST", "/api/v1/projects", map[string]any{"name": name, "stack": "api-go", "owner": "team-it",
		"parameters": map[string]any{"log_level": "debug", "metrics": false}})
	wait("création", code, task)
	repo := "apps/" + name
	if _, ok := forge.File(repo, "catalog-info.yaml"); !ok {
		t.Fatalf("fiche absente du repo %s", repo)
	}
	if c, _ := forge.File(repo, "main.go"); !strings.Contains(c, name+" debug") {
		t.Errorf("variables non remplacées : %q", c)
	}
	if _, ok := forge.File(repo, "metrics.go"); ok {
		t.Error("metrics.go créé alors que metrics vaut false")
	}
	if p, err := db.GetProjectByName(name); err != nil || p.Stack != "api-go" || p.StackParameters["log_level"] != "debug" ||
		p.StackParameters["metrics"] != false {
		t.Errorf("projet : %+v, %v", p, err)
	}

	// 2. La fiche déclare les variables par environnement, dont un secret
	fiche := `apiVersion: symphony/v1
kind: Service
metadata: {name: ` + name + `, owner: team-it}
spec:
  repo: http://forge/platform/symphony-stacks # faux : l'action vise le repo du projet
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
	// La promotion dit sa cible (affichée par l'activité)
	list, _ := db.ListTasks(database.TaskFilter{Resource: name, Limit: 50})
	promoted := false
	for _, tk := range list {
		if bytes.Contains(tk.Result, []byte("s3cr3t-db")) || bytes.Contains(tk.Params, []byte("s3cr3t-db")) {
			t.Errorf("secret dans la tâche #%d", tk.ID)
		}
		var res struct {
			Target struct {
				Environment  string `json:"environment"`
				PromotedFrom string `json:"promoted_from"`
			} `json:"target"`
		}
		_ = json.Unmarshal(tk.Result, &res)
		promoted = promoted || res.Target.Environment == "production" && res.Target.PromotedFrom == "staging"
	}
	if !promoted {
		t.Error("tâche de promotion sans sa cible (target)")
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
	if tk, _ := db.GetTask(int64(task["id"].(float64))); tk != nil {
		var params struct{ Repo string }
		if json.Unmarshal(tk.Params, &params); params.Repo != repo {
			t.Errorf("action lancée hors du repo du projet : %s", tk.Params)
		}
	}
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
		deps, _ := rt.List()
		for _, d := range deps {
			if d.Project == name {
				t.Errorf("déploiement restant sur %s : %v", rt.ID(), d)
			}
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

	// 8. Une stack avec son infra : appliquée avec le service, ses variables
	// lues dans les secrets qu'elle crée (le refus sur Docker est dans
	// handlers.TestCheckInfraRuntimes)
	withDB := name + "-db"
	code, task = call("POST", "/api/v1/projects", map[string]any{"name": withDB, "stack": "api-db", "owner": "team-it"})
	wait("création avec infra", code, task)
	if f, _ := forge.File("apps/"+withDB, "catalog-info.yaml"); !strings.Contains(f, "kubernetes: deploy/infra") {
		t.Errorf("infra absente de la fiche :\n%s", f)
	}
	syncer.ForceSync()
	code, img = call("GET", "/api/v1/projects/"+withDB+"/image", nil)
	tags, _ = img["tags"].([]any)
	if code != http.StatusOK || len(tags) == 0 {
		t.Fatalf("image : %d %v", code, img)
	}
	code, task = call("POST", "/api/v1/deployments", map[string]any{"project_name": withDB, "environment": "staging",
		"image": img["image"], "tag": tags[0]})
	wait("staging avec infra", code, task)
	if in := k8s.LastInfra[withDB+"-staging"]; in == nil || in.Files["db.yaml"] == "" || in.Env["DATABASE_URL"].Secret != "db-app" {
		t.Errorf("infra transmise au runtime : %+v", in)
	}
	code, task = call("DELETE", "/api/v1/projects/"+withDB, nil)
	wait("suppression avec infra", code, task)

	// 9. Un service existant, hors de Symphony, découvert par sa fiche
	// Backstage : il se déploie, et le retirer de Symphony garde son repo
	legacy := name + "-legacy"
	legacyRepo := "legacy/" + legacy
	forge.Seed(legacyRepo, map[string]string{"catalog-info.yaml": `apiVersion: backstage.io/v1alpha1
kind: Component
metadata:
  name: ` + legacy + `
  annotations: {symphony.io/port: "9090"}
spec: {type: service, lifecycle: production, owner: group:default/team-it}
`})
	src := gitops.DiscoverySource{SCM: "gitlab", Group: "legacy", CI: "gitlab-ci", Registry: registry.ID()}
	discovery.SetSettings(gitops.DiscoverySettings{Sources: []gitops.DiscoverySource{src}})
	discovery.Scan()
	p, err := db.GetProjectByName(legacy)
	if err != nil || p.DiscoveredFrom != "gitlab/legacy" || p.Team != "team-it" || p.Port != 9090 || p.RepoPath != legacyRepo {
		t.Fatalf("projet découvert : %+v %v (rapport %+v)", p, err, discovery.Report())
	}
	if _, ok := store.Get(legacy); !ok {
		t.Error("fiche du projet découvert absente du catalogue")
	}
	code, img = call("GET", "/api/v1/projects/"+legacy+"/image", nil)
	tags, _ = img["tags"].([]any)
	if code != http.StatusOK || len(tags) == 0 {
		t.Fatalf("image du projet découvert : %d %v", code, img)
	}
	code, task = call("POST", "/api/v1/deployments", map[string]any{"project_name": legacy, "environment": "staging",
		"image": img["image"], "tag": tags[0]})
	wait("déploiement d'un projet découvert", code, task)
	// Encore dans son groupe : le retirer ne servirait à rien
	if code, out := call("DELETE", "/api/v1/projects/"+legacy, nil); code != http.StatusConflict {
		t.Errorf("retrait d'un projet encore découvert : %d %v", code, out)
	}
	src.Exclude = []string{legacyRepo}
	discovery.SetSettings(gitops.DiscoverySettings{Sources: []gitops.DiscoverySource{src}})
	discovery.Scan()
	if rep := discovery.Report(); len(rep.Orphans) != 1 || rep.Orphans[0].Project != legacy {
		t.Errorf("orphelins : %+v", rep.Orphans)
	}
	code, task = call("DELETE", "/api/v1/projects/"+legacy, nil)
	wait("retrait d'un projet découvert", code, task)
	if _, err := db.GetProjectByName(legacy); err == nil {
		t.Error("projet découvert toujours en base")
	}
	if _, ok := forge.File(legacyRepo, "catalog-info.yaml"); !ok {
		t.Error("repo d'un projet découvert supprimé")
	}
	if tags, _ := registry.ListTags(legacyRepo); len(tags) == 0 {
		t.Error("images d'un projet découvert supprimées")
	}
	if deps, _ := docker.List(); slices.ContainsFunc(deps, func(d provider.Deployment) bool { return d.Project == legacy }) {
		t.Error("déploiement d'un projet découvert resté en place")
	}

	// Page Administration : l'audit a tout vu, les stacks sont chargées
	code, status := call("GET", "/api/v1/admin/status", nil)
	loaded, _ := status["stacks"].(map[string]any)
	if code != http.StatusOK || loaded == nil || len(loaded["loaded"].([]any)) == 0 || len(status["providers"].([]any)) == 0 {
		t.Errorf("admin/status : %d %v", code, status)
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
