package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"sort"
	"sync"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/provider"
)

// memStore : Store en mémoire ; chaque lecture renvoie une copie, comme une base
type memStore struct {
	mu    sync.Mutex
	tasks map[int64]database.Task
	next  int64
}

func newMemStore() *memStore { return &memStore{tasks: map[int64]database.Task{}} }

func clone(t database.Task) database.Task {
	if t.Wait != nil {
		w := *t.Wait
		t.Wait = &w
	}
	t.Steps = append([]string(nil), t.Steps...)
	t.Result = append(json.RawMessage(nil), t.Result...)
	return t
}

func (s *memStore) CreateTask(t *database.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.next++
	t.ID, t.CreatedAt = s.next, time.Now()
	s.tasks[t.ID] = clone(*t)
	return nil
}

func (s *memStore) UpdateTask(t *database.Task) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.tasks[t.ID]; !ok {
		return database.ErrTaskNotFound
	}
	s.tasks[t.ID] = clone(*t)
	return nil
}

func (s *memStore) GetTask(id int64) (*database.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t, ok := s.tasks[id]
	if !ok {
		return nil, database.ErrTaskNotFound
	}
	c := clone(t)
	return &c, nil
}

func (s *memStore) ListTasks(f database.TaskFilter) ([]database.Task, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []database.Task
	for _, t := range s.tasks {
		if (f.Resource == "" || t.Resource == f.Resource) && (!f.Unfinished || !t.Finished()) {
			out = append(out, clone(t))
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID > out[j].ID })
	return out, nil
}

// fakeCI : pipelines par repo, modifiables par le test
type fakeCI struct {
	provider.CIProvider
	mu        sync.Mutex
	pipelines map[string][]provider.Pipeline // repo → plus récentes d'abord
}

func (f *fakeCI) ID() string { return "ci" }

func (f *fakeCI) set(repo string, p provider.Pipeline) {
	f.mu.Lock()
	defer f.mu.Unlock()
	list := f.pipelines[repo]
	for i := range list {
		if list[i].ID == p.ID {
			list[i] = p
			return
		}
	}
	f.pipelines[repo] = append([]provider.Pipeline{p}, list...)
}

func (f *fakeCI) GetPipeline(repo, id string) (*provider.Pipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, p := range f.pipelines[repo] {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, provider.ErrNotFound
}

func (f *fakeCI) ListPipelines(repo string, limit int) ([]provider.Pipeline, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]provider.Pipeline(nil), f.pipelines[repo]...), nil
}

func newTestRunner(store Store) (*Runner, *fakeCI) {
	ci := &fakeCI{pipelines: map[string][]provider.Pipeline{}}
	reg := provider.NewRegistry()
	reg.RegisterCI(ci, true)
	return NewRunner(store, reg), ci
}

// waitFor attend que la tâche id remplisse cond
func waitFor(t *testing.T, s Store, id int64, cond func(*database.Task) bool) *database.Task {
	t.Helper()
	deadline := time.Now().Add(3 * time.Second)
	for {
		task, err := s.GetTask(id)
		if err == nil && cond(task) {
			return task
		}
		if time.Now().After(deadline) {
			t.Fatalf("tâche %d : condition jamais remplie ; état %+v (err %v)", id, task, err)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func finished(t *database.Task) bool { return t.Finished() }

func TestEtapesEnchaineesEtResultats(t *testing.T) {
	store := newMemStore()
	r, _ := newTestRunner(store)
	var order []string
	r.Register(Definition{Kind: "demo", Steps: []Step{
		{Label: "un", Run: func(ctx context.Context, x *Exec) error {
			var p struct{ Name string }
			if err := x.Params(&p); err != nil {
				return err
			}
			order = append(order, "un:"+p.Name)
			x.Set("repo", "root/"+p.Name)
			x.Progress("repo créé")
			return nil
		}},
		{Label: "deux", Run: func(ctx context.Context, x *Exec) error {
			var repo string
			x.Get("repo", &repo)
			order = append(order, "deux:"+repo)
			return nil
		}},
	}})
	events, unsubscribe := r.Hub().Subscribe()
	defer unsubscribe()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx, 2)

	task, err := r.Submit("demo", "api", map[string]string{"Name": "api"}, "bob")
	if err != nil {
		t.Fatal(err)
	}
	if task.Status != database.TaskPending || len(task.Steps) != 2 {
		t.Errorf("tâche soumise : %+v", task)
	}
	done := waitFor(t, store, task.ID, finished)
	if done.Status != database.TaskSuccess || done.Step != 2 || done.FinishedAt == nil {
		t.Errorf("tâche finie : %+v", done)
	}
	if len(order) != 2 || order[0] != "un:api" || order[1] != "deux:root/api" {
		t.Errorf("ordre = %v", order)
	}
	var result map[string]string
	json.Unmarshal(done.Result, &result)
	if result["repo"] != "root/api" {
		t.Errorf("résultat = %s", done.Result)
	}

	// Les abonnés voient passer PENDING, RUNNING, l'avancement et SUCCESS
	seen := map[string]bool{}
	timeout := time.After(time.Second)
	for !seen[database.TaskSuccess] {
		select {
		case ev := <-events:
			seen[ev.Status] = true
			if ev.Detail == "repo créé" {
				seen["progress"] = true
			}
		case <-timeout:
			t.Fatalf("événements reçus : %v", seen)
		}
	}
	for _, want := range []string{database.TaskPending, database.TaskRunning, "progress"} {
		if !seen[want] {
			t.Errorf("événement %s jamais publié (%v)", want, seen)
		}
	}
}

func TestEchecEtPanique(t *testing.T) {
	store := newMemStore()
	r, _ := newTestRunner(store)
	var finishedTasks []database.Task
	var mu sync.Mutex
	r.OnFinish = func(t database.Task) { mu.Lock(); finishedTasks = append(finishedTasks, t); mu.Unlock() }
	ranAfter := false
	r.Register(Definition{Kind: "ko", Steps: []Step{
		{Label: "casse", Run: func(context.Context, *Exec) error { return errors.New("repo déjà pris") }},
		{Label: "jamais", Run: func(context.Context, *Exec) error { ranAfter = true; return nil }},
	}})
	r.Register(Definition{Kind: "panique", Steps: []Step{
		{Label: "boum", Run: func(context.Context, *Exec) error { panic("nil map") }},
	}})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx, 1)

	ko, _ := r.Submit("ko", "a", nil, "bob")
	done := waitFor(t, store, ko.ID, finished)
	if done.Status != database.TaskFailed || done.Error != "repo déjà pris" || done.Step != 0 || ranAfter {
		t.Errorf("tâche en échec : %+v (étape suivante lancée : %v)", done, ranAfter)
	}
	p, _ := r.Submit("panique", "b", nil, "bob")
	if done := waitFor(t, store, p.ID, finished); done.Status != database.TaskFailed {
		t.Errorf("panique : %+v", done)
	}
	mu.Lock()
	defer mu.Unlock()
	if len(finishedTasks) != 2 {
		t.Errorf("OnFinish appelé %d fois", len(finishedTasks))
	}
	if _, err := r.Submit("inconnu", "c", nil, "bob"); err == nil {
		t.Error("type inconnu accepté")
	}
}

// pipelineDef : une étape qui attend la prochaine pipeline du repo, puis une étape finale
func pipelineDef(final *bool) Definition {
	return Definition{Kind: "build", Steps: []Step{
		{Label: "push", Run: func(_ context.Context, x *Exec) error {
			x.WaitPipeline("", "root/api", "", "", time.Now())
			return nil
		}},
		{Label: "fin", Run: func(context.Context, *Exec) error { *final = true; return nil }},
	}}
}

func TestAttenteDePipelineParWebhook(t *testing.T) {
	store := newMemStore()
	r, _ := newTestRunner(store)
	final := false
	r.Register(pipelineDef(&final))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx, 1)

	task, _ := r.Submit("build", "api", nil, "bob")
	waiting := waitFor(t, store, task.ID, func(t *database.Task) bool { return t.Wait != nil })
	if waiting.Status != database.TaskRunning || waiting.Step != 0 {
		t.Errorf("en attente : %+v", waiting)
	}

	r.PipelineEvent("root/autre", "1", "success", "") // autre repo : ignoré
	r.PipelineEvent("root/api", "42", "running", "http://gitlab/p/42")
	running := waitFor(t, store, task.ID, func(t *database.Task) bool { return t.Wait != nil && t.Wait.Status == "running" })
	if running.Wait.PipelineID != "42" || running.Detail != "pipeline #42 : running" {
		t.Errorf("pipeline en cours : %+v / %+v", running, running.Wait)
	}
	r.PipelineEvent("root/api", "43", "success", "") // pas celle qu'on attend
	if got, _ := store.GetTask(task.ID); got.Finished() {
		t.Fatal("une autre pipeline a terminé la tâche")
	}
	r.PipelineEvent("root/api", "42", "success", "")
	done := waitFor(t, store, task.ID, finished)
	if done.Status != database.TaskSuccess || !final {
		t.Errorf("après la pipeline : %+v (étape finale : %v)", done, final)
	}
}

func TestPipelineEchoueeFaitEchouerLaTache(t *testing.T) {
	store := newMemStore()
	r, _ := newTestRunner(store)
	final := false
	r.Register(pipelineDef(&final))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx, 1)
	task, _ := r.Submit("build", "api", nil, "bob")
	waitFor(t, store, task.ID, func(t *database.Task) bool { return t.Wait != nil })
	r.PipelineEvent("root/api", "7", "failed", "")
	done := waitFor(t, store, task.ID, finished)
	if done.Status != database.TaskFailed || done.Error != "pipeline #7 terminée en failed" || final {
		t.Errorf("tâche : %+v", done)
	}
}

// Un redémarrage pendant une étape, puis pendant une attente de pipeline :
// un nouveau runner sur la même base reprend là où le premier s'était arrêté
func TestRepriseApresRedemarrage(t *testing.T) {
	store := newMemStore()
	counts := map[string]int{}
	var mu sync.Mutex
	count := func(s string) { mu.Lock(); counts[s]++; mu.Unlock() }
	blocked := make(chan struct{})

	def := func(block bool) Definition {
		return Definition{Kind: "long", Steps: []Step{
			{Label: "repo", Run: func(_ context.Context, x *Exec) error {
				count("repo")
				x.Set("repo", "root/api")
				return nil
			}},
			{Label: "lent", Run: func(ctx context.Context, x *Exec) error {
				count("lent")
				if block {
					close(blocked)
					<-ctx.Done() // Symphony s'arrête au milieu de l'étape
					return ctx.Err()
				}
				x.WaitPipeline("", "root/api", "", "", time.Now().Add(-time.Minute))
				return nil
			}},
			{Label: "fin", Run: func(context.Context, *Exec) error { count("fin"); return nil }},
		}}
	}

	// Premier Symphony : arrêté pendant l'étape « lent »
	r1, _ := newTestRunner(store)
	r1.Register(def(true))
	ctx1, stop1 := context.WithCancel(context.Background())
	r1.Start(ctx1, 1)
	task, _ := r1.Submit("long", "api", nil, "bob")
	<-blocked
	stop1()
	time.Sleep(20 * time.Millisecond)
	if got, _ := store.GetTask(task.ID); got.Status != database.TaskRunning || got.Step != 1 {
		t.Fatalf("après l'arrêt : %+v", got)
	}

	// Deuxième Symphony : reprend « lent » (pas « repo »), qui attend une pipeline
	r2, ci := newTestRunner(store)
	r2.Register(def(false))
	ctx2, stop2 := context.WithCancel(context.Background())
	r2.Start(ctx2, 1)
	waitFor(t, store, task.ID, func(t *database.Task) bool { return t.Wait != nil })
	stop2()

	// Troisième Symphony : la tâche en attente survit ; le sondage trouve la
	// pipeline (pas de webhook) et la tâche se termine
	r3, _ := newTestRunner(store)
	r3.providers = r2.providers
	r3.Register(def(false))
	ctx3, stop3 := context.WithCancel(context.Background())
	defer stop3()
	r3.Start(ctx3, 1)
	ci.set("root/api", provider.Pipeline{ID: "5", Status: "success", CreatedAt: time.Now()})
	r3.PollOnce()
	done := waitFor(t, store, task.ID, finished)

	mu.Lock()
	defer mu.Unlock()
	if done.Status != database.TaskSuccess || counts["repo"] != 1 || counts["lent"] != 2 || counts["fin"] != 1 {
		t.Errorf("tâche %+v ; exécutions %v (repo une fois, lent rejouée une fois)", done, counts)
	}
	var result map[string]string
	json.Unmarshal(done.Result, &result)
	if result["repo"] != "root/api" {
		t.Errorf("résultat perdu au redémarrage : %s", done.Result)
	}
}

func TestSondage(t *testing.T) {
	store := newMemStore()
	r, ci := newTestRunner(store)
	final := false
	r.Register(pipelineDef(&final))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx, 1)

	// Une pipeline antérieure à l'attente n'est pas celle qu'on attend
	ci.set("root/api", provider.Pipeline{ID: "1", Status: "success", CreatedAt: time.Now().Add(-time.Hour)})
	task, _ := r.Submit("build", "api", nil, "bob")
	waitFor(t, store, task.ID, func(t *database.Task) bool { return t.Wait != nil })
	r.PollOnce()
	if got, _ := store.GetTask(task.ID); got.Finished() || got.Wait.PipelineID != "" {
		t.Fatalf("ancienne pipeline prise : %+v", got.Wait)
	}

	ci.set("root/api", provider.Pipeline{ID: "2", Status: "running", CreatedAt: time.Now()})
	r.PollOnce()
	waitFor(t, store, task.ID, func(t *database.Task) bool { return t.Wait != nil && t.Wait.PipelineID == "2" })
	ci.set("root/api", provider.Pipeline{ID: "2", Status: "success", CreatedAt: time.Now()})
	r.PollOnce()
	if done := waitFor(t, store, task.ID, finished); done.Status != database.TaskSuccess || !final {
		t.Errorf("tâche : %+v", done)
	}
}

func TestDelaiDAttenteDepasse(t *testing.T) {
	store := newMemStore()
	r, _ := newTestRunner(store)
	r.WaitTimeout = 0
	final := false
	r.Register(pipelineDef(&final))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	r.Start(ctx, 1)
	task, _ := r.Submit("build", "api", nil, "bob")
	waitFor(t, store, task.ID, func(t *database.Task) bool { return t.Wait != nil })
	r.PollOnce()
	done := waitFor(t, store, task.ID, finished)
	if done.Status != database.TaskFailed || done.Wait != nil {
		t.Errorf("tâche : %+v", done)
	}
}

func TestHubAbonneLentDeconnecte(t *testing.T) {
	h := NewHub()
	ch, unsubscribe := h.Subscribe()
	for i := 0; i < 100; i++ {
		h.Publish(database.Task{ID: int64(i)})
	}
	n := 0
	for range ch { // fermé après débordement
		n++
	}
	if n != 64 {
		t.Errorf("%d événements avant fermeture, attendu 64", n)
	}
	unsubscribe() // sans effet (déjà retiré), ne doit pas paniquer
}
