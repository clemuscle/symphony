package handlers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/database/dbtest"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/notify"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/fake"
	"github.com/clemuscle/symphony/internal/provider/notifications/slack"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
)

var (
	lina = &auth.User{Name: "lina", Email: "lina@test", Role: auth.RoleLead, Teams: []string{"team-payments"}}
	dave = &auth.User{Name: "dave", Email: "dave@test", Role: auth.RoleLead, Teams: []string{"team-identity"}}
)

// TestApprovals : en production (approbation par un lead), la demande d'un
// developer attend ; seuls un lead de l'équipe ou un admin l'approuvent, le
// demandeur peut la retirer, un lead passe sans attendre. L'équipe est
// prévenue dans son canal ; l'historique dit qui a approuvé, et le retour
// arrière redéploie la version précédente.
func TestApprovals(t *testing.T) {
	db := dbtest.Open(t)
	rt := fake.NewRuntime("docker-local", false)
	providers := provider.NewRegistry()
	providers.RegisterRuntime(rt, true)
	envs := environments.NewHolder([]environments.Environment{
		{Name: "staging", Runtime: "docker-local"},
		{Name: "production", Runtime: "docker-local", Protected: true, Approval: environments.ApprovalLead},
	})
	stacks := gitops.NewStackLoader(providers, nil, gitops.StackSource{})

	// Canal Slack de l'équipe : un faux webhook qui garde les messages
	messages := make(chan string, 20)
	hook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct{ Text string }
		json.NewDecoder(r.Body).Decode(&body)
		messages <- body.Text
	}))
	defer hook.Close()
	channel, err := slack.New("slack-payments", hook.URL)
	if err != nil {
		t.Fatal(err)
	}
	providers.RegisterNotifications(channel)
	notifier := notify.New(providers, &notify.Settings{URL: "https://symphony.test", Teams: map[string][]string{"team-payments": {"slack-payments"}}})
	expect := func(want string) {
		t.Helper()
		select {
		case m := <-messages:
			if !strings.Contains(m, want) {
				t.Errorf("notification %q, attendu %q", m, want)
			}
		case <-time.After(5 * time.Second):
			t.Errorf("pas de notification %q", want)
		}
	}

	runner := tasks.NewRunner(db, providers)
	RegisterTasks(runner, TaskDeps{Providers: providers, DB: db, Stacks: stacks, Envs: envs, Store: catalogue.NewStore(), Notify: notifier})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	runner.StartWorkers(ctx, 1) // sans reprendre les tâches des autres tests

	name := fmt.Sprintf("appr-%d", time.Now().UnixNano()%1e9)
	if err := db.CreateProject(&database.Project{Name: name, RepoPath: "apps/" + name, Team: "team-payments", Port: 8080}); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		db.DeleteProject(name, "apps/"+name)
		db.DeleteDeployments(name)
		db.Exec(`DELETE FROM tasks WHERE resource = $1`, name)
	})

	r := chi.NewRouter()
	r.Post("/deployments", Deploy(providers, db, stacks, envs, runner))
	r.Post("/projects/{name}/promote", Promote(providers, db, stacks, envs, runner))
	r.Post("/projects/{name}/rollback", Rollback(providers, db, stacks, envs, runner))
	r.Get("/approvals", ListApprovals(db))
	r.Get("/releases", ListReleases(db))
	r.Post("/tasks/{id}/approve", DecideTask(db, runner, notifier, true))
	r.Post("/tasks/{id}/reject", DecideTask(db, runner, notifier, false))

	call := func(u *auth.User, method, path string, body any) (int, []byte) {
		t.Helper()
		var buf bytes.Buffer
		if body != nil {
			json.NewEncoder(&buf).Encode(body)
		}
		req := httptest.NewRequest(method, path, &buf)
		req = req.WithContext(auth.ContextWithUser(req.Context(), u))
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		out, _ := io.ReadAll(rec.Body)
		return rec.Code, out
	}
	// submitted : la tâche lancée (202)
	submitted := func(step string, code int, body []byte) int64 {
		t.Helper()
		var task database.Task
		if code != http.StatusAccepted || json.Unmarshal(body, &task) != nil {
			t.Fatalf("%s : %d %s", step, code, body)
		}
		return task.ID
	}
	// until : attend que la tâche id vérifie ok
	until := func(step string, id int64, ok func(*database.Task) bool) *database.Task {
		t.Helper()
		deadline := time.Now().Add(10 * time.Second)
		for time.Now().Before(deadline) {
			got, err := db.GetTask(id)
			if err != nil {
				t.Fatal(err)
			}
			if ok(got) {
				return got
			}
			time.Sleep(30 * time.Millisecond)
		}
		got, _ := db.GetTask(id)
		t.Fatalf("%s : tâche #%d %s (%s, %s)", step, id, got.Status, got.Detail, got.Error)
		return nil
	}
	finished := func(tk *database.Task) bool { return tk.Finished() }
	awaiting := func(tk *database.Task) bool { return tk.Wait != nil && tk.Wait.Approval }
	deploy := func(u *auth.User, tag string) {
		t.Helper()
		code, body := call(u, "POST", "/deployments", map[string]any{"project_name": name, "environment": "staging", "image": "img", "tag": tag})
		id := submitted("staging "+tag, code, body)
		if tk := until("staging "+tag, id, finished); tk.Status != database.TaskSuccess {
			t.Fatalf("staging %s : %s", tag, tk.Error)
		}
		expect("Déployé : " + name + " en staging")
	}

	// Staging, sans approbation : v1 puis v2
	deploy(bob, "1")
	deploy(bob, "2")

	// bob (developer) promeut : la tâche attend un lead, l'équipe est prévenue
	code, body := call(bob, "POST", "/projects/"+name+"/promote", map[string]any{"to": "production"})
	promotion := submitted("promotion", code, body)
	until("promotion en attente", promotion, awaiting)
	expect("Approbation demandée : " + name + " en production")

	code, body = call(bob, "GET", "/approvals", nil)
	var pending []Approval
	json.Unmarshal(body, &pending)
	found := false
	for _, a := range pending {
		if a.Task.ID == promotion {
			found = true
			if a.CanApprove || !a.CanReject || a.Team != "team-payments" {
				t.Errorf("bob voit %+v : il ne peut que retirer sa demande", a)
			}
		}
	}
	if code != http.StatusOK || !found {
		t.Errorf("approbations de bob : %d %s", code, body)
	}
	for _, c := range []struct {
		who  string
		u    *auth.User
		want int
	}{
		{"bob (le demandeur, developer)", bob, 403},
		{"eve (developer d'une autre équipe)", eve, 403},
		{"dave (lead d'une autre équipe)", dave, 403},
	} {
		if code, body := call(c.u, "POST", fmt.Sprintf("/tasks/%d/approve", promotion), nil); code != c.want {
			t.Errorf("%s approuve : %d %s", c.who, code, body)
		}
	}
	// Le projet reste occupé tant que la demande attend
	if code, body := call(bob, "POST", "/deployments", map[string]any{"project_name": name, "environment": "staging", "image": "img", "tag": "3"}); code != 409 ||
		!strings.Contains(string(body), "en attente d'approbation") {
		t.Errorf("déploiement pendant l'attente : %d %s", code, body)
	}
	if code, body := call(eve, "POST", fmt.Sprintf("/tasks/%d/reject", promotion), nil); code != 403 {
		t.Errorf("eve refuse : %d %s", code, body)
	}
	if code, body := call(lina, "POST", fmt.Sprintf("/tasks/%d/approve", promotion), map[string]any{"comment": strings.Repeat("x", 501)}); code != 400 {
		t.Errorf("commentaire trop long : %d %s", code, body)
	}

	// lina (lead de l'équipe) approuve : la promotion part
	if code, body := call(lina, "POST", fmt.Sprintf("/tasks/%d/approve", promotion), map[string]any{"comment": "ok pour la v2"}); code != 200 {
		t.Fatalf("lina approuve : %d %s", code, body)
	}
	expect("Approuvé : " + name + " en production")
	if tk := until("promotion approuvée", promotion, finished); tk.Status != database.TaskSuccess {
		t.Fatalf("promotion : %s", tk.Error)
	}
	expect("Déployé : " + name + " en production")
	if code, body := call(lina, "POST", fmt.Sprintf("/tasks/%d/approve", promotion), nil); code != 409 {
		t.Errorf("deuxième approbation : %d %s", code, body)
	}
	prod := "production"
	releases, _ := db.ListReleases(database.ReleaseFilter{Project: name, Environment: &prod})
	if len(releases) != 1 || releases[0].Tag != "2" || releases[0].ApprovedBy != "lina@test" ||
		releases[0].Origin != database.OriginPromotion || releases[0].Status != database.ReleaseRunning || releases[0].TaskID != promotion {
		t.Errorf("historique de production : %+v", releases)
	}

	// Retour arrière en staging (pas d'approbation) : v2 → v1
	code, body = call(bob, "POST", "/projects/"+name+"/rollback", map[string]any{"environment": "staging"})
	id := submitted("retour arrière en staging", code, body)
	if tk := until("retour arrière en staging", id, finished); tk.Status != database.TaskSuccess {
		t.Fatalf("retour arrière : %s", tk.Error)
	}
	expect("Déployé : " + name + " en staging")
	if d, _ := rt.Status(name + "-staging"); d == nil || d.Image != "img:1" {
		t.Errorf("staging après retour arrière : %+v", d)
	}

	// Retour arrière en production : rien avant la v2
	if code, body := call(bob, "POST", "/projects/"+name+"/rollback", map[string]any{"environment": "production"}); code != 409 {
		t.Errorf("retour arrière sans version précédente : %d %s", code, body)
	}

	// lina promeut elle-même la v1 (après le retour arrière) : pas d'attente
	code, body = call(lina, "POST", "/projects/"+name+"/promote", map[string]any{"to": "production"})
	id = submitted("promotion par lina", code, body)
	if tk := until("promotion par lina", id, finished); tk.Status != database.TaskSuccess || tk.Wait != nil {
		t.Fatalf("promotion par un lead : %s", tk.Error)
	}
	expect("Déployé : " + name + " en production")

	// bob revient à la v2 en production : approbation demandée, puis il retire sa demande
	code, body = call(bob, "POST", "/projects/"+name+"/rollback", map[string]any{"environment": "production"})
	id = submitted("retour arrière en production", code, body)
	until("retour arrière en attente", id, awaiting)
	expect("Approbation demandée")
	if code, body := call(bob, "POST", fmt.Sprintf("/tasks/%d/reject", id), nil); code != 200 {
		t.Fatalf("bob retire sa demande : %d %s", code, body)
	}
	if tk := until("demande retirée", id, finished); tk.Status != database.TaskFailed || !strings.Contains(tk.Error, "refusé par bob@test") {
		t.Errorf("demande retirée : %s %s", tk.Status, tk.Error)
	}
	expect("bob@test retire sa demande")

	// Matrice : la dernière version de chaque environnement
	code, body = call(carol, "GET", "/releases?project="+name+"&latest=true", nil)
	var latest []database.Deployment
	json.Unmarshal(body, &latest)
	got := map[string]string{}
	for _, l := range latest {
		got[l.Environment] = l.Tag + "/" + l.Origin
	}
	if code != 200 || got["staging"] != "1/rollback" || got["production"] != "1/promotion" || len(got) != 2 {
		t.Errorf("matrice : %d %v", code, got)
	}
	select {
	case m := <-messages:
		t.Errorf("notification en trop : %q", m)
	case <-time.After(200 * time.Millisecond):
	}
}
