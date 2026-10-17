package handlers

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/database/dbtest"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/fake"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
)

var (
	bob   = &auth.User{Name: "bob", Email: "bob@test", Role: auth.RoleDeveloper, Teams: []string{"team-payments"}}
	eve   = &auth.User{Name: "eve", Email: "eve@test", Role: auth.RoleDeveloper, Teams: []string{"team-identity"}}
	carol = &auth.User{Name: "carol", Email: "carol@test", Role: auth.RoleViewer, Teams: []string{"team-payments"}}
	alice = &auth.User{Name: "alice", Email: "alice@test", Role: auth.RoleAdmin, Teams: []string{}}
)

// TestTeamRights : agir sur un projet (déployer, promouvoir, arrêter, lire les
// logs, relancer la pipeline) est réservé aux developers de son équipe et aux
// admins ; un environnement protégé ne reçoit que des promotions
func TestTeamRights(t *testing.T) {
	db := dbtest.Open(t)
	rt := fake.NewRuntime("docker-local", false)
	forge := fake.NewForge(nil)
	providers := provider.NewRegistry()
	providers.RegisterRuntime(rt, true)
	k8s := fake.NewRuntime("argocd-k8s", true)
	providers.RegisterRuntime(k8s, false)
	providers.RegisterCI(forge.CI(), true)
	envs := environments.NewHolder([]environments.Environment{
		{Name: "staging", Runtime: "docker-local"},
		{Name: "production", Runtime: "docker-local", Protected: true},
	})
	stacks := gitops.NewStackLoader(providers, nil, gitops.StackSource{}) // pas de stack : runtimes de la configuration
	runner := tasks.NewRunner(db, providers)                              // pas démarré : les tâches restent en file
	RegisterTasks(runner, TaskDeps{Providers: providers, DB: db, Stacks: stacks, Envs: envs})

	name := fmt.Sprintf("authz-%d", time.Now().UnixNano()%1e9)
	if err := db.CreateProject(&database.Project{Name: name, RepoPath: "apps/" + name, CIProvider: "gitlab-ci", Team: "team-payments"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DeleteProject(name, "apps/"+name) })
	// Les tâches lancées ici ne doivent pas être reprises par le runner d'un
	// autre test (même base)
	t.Cleanup(func() { db.Exec(`DELETE FROM tasks WHERE resource LIKE $1`, name+"%") })
	forge.SCM().CreateRepo(provider.CreateRepoRequest{Name: name, Namespace: "apps"})
	rt.Deploy(provider.DeployRequest{Name: name + "-staging", Project: name, Environment: "staging", Image: "img:1"})
	// Un second projet, qui tourne en latest en staging : pas de promotion
	mobile := name + "-latest"
	if err := db.CreateProject(&database.Project{Name: mobile, RepoPath: "apps/" + mobile, Team: "team-payments"}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.DeleteProject(mobile, "apps/"+mobile) })
	rt.Deploy(provider.DeployRequest{Name: mobile + "-staging", Project: mobile, Environment: "staging", Image: "img", Tag: "latest"})
	// Sur l'autre runtime : un déploiement retrouvé sans ?provider=, un
	// homonyme présent dans les deux
	k8s.Deploy(provider.DeployRequest{Name: name + "-production", Project: name, Environment: "production", Image: "img:1"})
	k8s.Deploy(provider.DeployRequest{Name: mobile + "-staging", Project: mobile, Environment: "staging", Image: "img"})

	r := chi.NewRouter()
	r.Post("/deployments", Deploy(providers, db, stacks, envs, runner))
	r.Post("/projects/{name}/promote", Promote(providers, db, stacks, envs, runner))
	r.Delete("/deployments/{id}", StopDeployment(providers, db))
	r.Get("/deployments/{id}/logs", GetLogs(providers, db))
	r.Post("/pipelines/trigger", TriggerPipeline(providers, db))

	call := func(u *auth.User, method, path string, body any) (int, string) {
		var buf bytes.Buffer
		if body != nil {
			json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req = req.WithContext(auth.ContextWithUser(req.Context(), u))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code, rec.Body.String()
	}
	deploy := func(env string) map[string]any {
		return map[string]any{"project_name": name, "environment": env, "image": "img", "tag": "2"}
	}
	logs := "/deployments/" + name + "-staging/logs?provider=docker-local"
	stop := "/deployments/" + name + "-staging?provider=docker-local"

	cases := []struct {
		who      string
		u        *auth.User
		method   string
		path     string
		body     any
		want     int
		contains string
	}{
		{"eve (autre équipe) déploie", eve, "POST", "/deployments", deploy("staging"), 403, "team-payments"},
		{"eve lit les logs", eve, "GET", logs, nil, 403, "réservé"},
		{"eve arrête", eve, "DELETE", stop, nil, 403, "réservé"},
		{"eve promeut", eve, "POST", "/projects/" + name + "/promote", map[string]any{"to": "production"}, 403, "réservé"},
		{"eve relance la pipeline", eve, "POST", "/pipelines/trigger", map[string]any{"project": name}, 403, "réservé"},
		{"carol (viewer de l'équipe) déploie", carol, "POST", "/deployments", deploy("staging"), 403, "réservé"},
		{"bob déploie en production", bob, "POST", "/deployments", deploy("production"), 403, "protégé"},
		{"bob sans environnement", bob, "POST", "/deployments", deploy(""), 400, "staging, production"},
		{"bob lit les logs", bob, "GET", logs, nil, 200, "prêt"},
		{"bob lit les logs sans provider", bob, "GET", "/deployments/" + name + "-production/logs", nil, 200, "prêt"},
		{"eve lit les logs sans provider", eve, "GET", "/deployments/" + name + "-production/logs", nil, 403, "réservé"},
		{"bob lit les logs d'un homonyme", bob, "GET", "/deployments/" + mobile + "-staging/logs", nil, 400, "plusieurs runtimes"},
		{"bob lit les logs d'un inconnu", bob, "GET", "/deployments/inconnu/logs", nil, 404, "introuvable"},
		{"bob promeut une image latest", bob, "POST", "/projects/" + mobile + "/promote", map[string]any{"to": "production"}, 409, "tag immuable"},
		{"bob relance la pipeline", bob, "POST", "/pipelines/trigger", map[string]any{"project": name}, 200, ""},
		{"bob déploie en staging", bob, "POST", "/deployments", deploy("staging"), 202, "deploy"},
		{"bob promeut (tâche déjà en cours)", bob, "POST", "/projects/" + name + "/promote", map[string]any{"to": "production"}, 409, "déjà en cours"},
		{"alice (admin) déploie un projet inconnu", alice, "POST", "/deployments", map[string]any{"project_name": "inconnu", "environment": "staging", "image": "i"}, 400, "pas d'environnement"},
		{"bob déploie un projet inconnu", bob, "POST", "/deployments", map[string]any{"project_name": "inconnu", "environment": "staging", "image": "i"}, 403, "réservé aux admins"},
	}
	for _, c := range cases {
		code, body := call(c.u, c.method, c.path, c.body)
		if code != c.want || !strings.Contains(body, c.contains) {
			t.Errorf("%s : %d %s ; attendu %d contenant %q", c.who, code, strings.TrimSpace(body), c.want, c.contains)
		}
	}
}
