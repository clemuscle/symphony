// Package tasks exécute les opérations longues de Symphony (créer un projet,
// déployer, supprimer, lancer une action Day-2) comme des tâches suivies :
// PENDING → RUNNING → SUCCESS | FAILED.
//
// Une tâche est une suite d'étapes. Après chaque étape, et à chaque résultat
// intermédiaire (Exec.Set), son état est enregistré en base : au redémarrage
// de Symphony, les tâches non terminées reprennent à l'étape où elles étaient.
// Une étape peut attendre la fin d'une pipeline CI sans occuper de goroutine :
// la tâche est mise en attente, puis relancée par un webhook entrant
// (PipelineEvent) ou, à défaut, par le sondage périodique du provider CI.
package tasks

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"runtime/debug"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/provider"
)

// Store : persistance des tâches (database.DB, ou un faux en test)
type Store interface {
	CreateTask(t *database.Task) error
	UpdateTask(t *database.Task) error
	GetTask(id int64) (*database.Task, error)
	ListTasks(f database.TaskFilter) ([]database.Task, error)
}

// Step : une étape d'une tâche. Run doit pouvoir être rejouée : après un
// redémarrage, l'étape interrompue est relancée depuis le début (les
// résultats déjà enregistrés avec Exec.Set permettent de sauter ce qui est fait).
type Step struct {
	Label string
	Run   func(ctx context.Context, x *Exec) error
}

// Definition : un type de tâche et ses étapes
type Definition struct {
	Kind  string
	Steps []Step
}

type Runner struct {
	store     Store
	providers *provider.Registry // résolu à chaque appel : la configuration peut changer
	hub       *Hub

	defs  map[string]Definition
	queue chan int64

	mu     sync.Mutex     // une transition d'état à la fois (worker, webhook, sondage)
	active map[int64]bool // tâches en cours d'exécution par un worker

	// PollInterval : sondage des pipelines attendues (filet de sécurité des webhooks)
	PollInterval time.Duration
	// WaitTimeout : au-delà, une pipeline attendue est considérée perdue
	WaitTimeout time.Duration
	// OnFinish est appelé quand une tâche se termine (audit des échecs…)
	OnFinish func(t database.Task)
}

func NewRunner(store Store, providers *provider.Registry) *Runner {
	return &Runner{
		store:        store,
		providers:    providers,
		hub:          NewHub(),
		defs:         map[string]Definition{},
		queue:        make(chan int64, 256),
		active:       map[int64]bool{},
		PollInterval: 15 * time.Second,
		WaitTimeout:  time.Hour,
	}
}

// Register déclare un type de tâche (avant Start)
func (r *Runner) Register(def Definition) { r.defs[def.Kind] = def }

// Hub : abonnements aux changements de tâches (Server-Sent Events)
func (r *Runner) Hub() *Hub { return r.hub }

// Submit enregistre une tâche et la met en file ; elle s'exécute en arrière-plan
func (r *Runner) Submit(kind, resource string, params any, user string) (*database.Task, error) {
	def, ok := r.defs[kind]
	if !ok {
		return nil, fmt.Errorf("type de tâche inconnu: %s", kind)
	}
	raw, err := json.Marshal(params)
	if err != nil {
		return nil, fmt.Errorf("paramètres de la tâche: %w", err)
	}
	labels := make([]string, len(def.Steps))
	for i, s := range def.Steps {
		labels[i] = s.Label
	}
	t := &database.Task{
		Kind:      kind,
		Resource:  resource,
		Status:    database.TaskPending,
		Steps:     labels,
		Params:    raw,
		Result:    json.RawMessage("{}"),
		CreatedBy: user,
	}
	if err := r.store.CreateTask(t); err != nil {
		return nil, fmt.Errorf("enregistrement de la tâche: %w", err)
	}
	r.hub.Publish(*t)
	r.enqueue(t.ID)
	return t, nil
}

// Active renvoie la tâche non terminée sur resource, s'il y en a une : une
// seule opération à la fois par projet
func (r *Runner) Active(resource string) (*database.Task, error) {
	tasks, err := r.store.ListTasks(database.TaskFilter{Resource: resource, Unfinished: true, Limit: 1})
	if err != nil || len(tasks) == 0 {
		return nil, err
	}
	return &tasks[0], nil
}

// Start reprend les tâches interrompues, lance les workers et le sondage des
// pipelines. Rend la main aussitôt ; ctx arrête tout.
func (r *Runner) Start(ctx context.Context, workers int) {
	for i := 0; i < workers; i++ {
		go r.worker(ctx)
	}
	unfinished, err := r.store.ListTasks(database.TaskFilter{Unfinished: true, Limit: 1000})
	if err != nil {
		slog.Error("tâches : reprise impossible", "error", err)
	}
	for _, t := range unfinished {
		if t.Wait == nil {
			slog.Info("tâche reprise", "id", t.ID, "kind", t.Kind, "resource", t.Resource, "step", t.Step)
			r.enqueue(t.ID)
		}
	}
	go r.poll(ctx)
}

func (r *Runner) enqueue(id int64) {
	select {
	case r.queue <- id:
	default:
		go func() { r.queue <- id }() // file pleine : ne pas bloquer l'appelant
	}
}

func (r *Runner) worker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case id := <-r.queue:
			r.execute(ctx, id)
		}
	}
}

// execute fait avancer la tâche id jusqu'à sa fin ou jusqu'à une attente
func (r *Runner) execute(ctx context.Context, id int64) {
	r.mu.Lock()
	if r.active[id] {
		r.mu.Unlock()
		return
	}
	r.active[id] = true
	t, err := r.store.GetTask(id)
	r.mu.Unlock()
	defer func() {
		r.mu.Lock()
		delete(r.active, id)
		r.mu.Unlock()
	}()
	if err != nil {
		slog.Error("tâche illisible", "id", id, "error", err)
		return
	}
	if t.Finished() || t.Wait != nil {
		return
	}

	def, ok := r.defs[t.Kind]
	if !ok {
		r.finish(t, fmt.Errorf("type de tâche inconnu: %s", t.Kind))
		return
	}
	if t.Status == database.TaskPending {
		t.Status = database.TaskRunning
		r.save(t)
	}

	x, err := newExec(r, t)
	if err != nil {
		r.finish(t, err)
		return
	}
	for t.Step < len(def.Steps) {
		err := runStep(ctx, def.Steps[t.Step], x)
		if ctx.Err() != nil {
			return // arrêt de Symphony : la tâche reprendra à cette étape
		}
		if err != nil {
			r.finish(t, err)
			return
		}
		if x.wait != nil {
			r.mu.Lock() // un webhook arrivé juste après voit l'attente enregistrée
			t.Wait, x.wait = x.wait, nil
			t.Detail = "en attente de la pipeline"
			r.save(t)
			r.mu.Unlock()
			return
		}
		t.Step++
		t.Detail = ""
		r.save(t)
	}
	r.finish(t, nil)
}

// runStep exécute une étape ; une panique devient une erreur de la tâche
func runStep(ctx context.Context, s Step, x *Exec) (err error) {
	defer func() {
		if p := recover(); p != nil {
			slog.Error("panique dans une tâche", "step", s.Label, "panic", p, "stack", string(debug.Stack()))
			err = fmt.Errorf("erreur interne à l'étape %q", s.Label)
		}
	}()
	return s.Run(ctx, x)
}

// save enregistre la tâche et la publie aux abonnés. Le worker qui exécute une
// tâche est seul à la modifier ; une tâche en attente n'est modifiée que sous r.mu.
func (r *Runner) save(t *database.Task) {
	if err := r.store.UpdateTask(t); err != nil {
		slog.Error("enregistrement de la tâche", "id", t.ID, "error", err)
	}
	r.hub.Publish(*t)
}

// finish termine la tâche : SUCCESS si err est nil, FAILED sinon
func (r *Runner) finish(t *database.Task, err error) {
	now := time.Now()
	t.FinishedAt = &now
	t.Wait = nil
	if err != nil {
		t.Status = database.TaskFailed
		t.Error = err.Error()
		slog.Warn("tâche échouée", "id", t.ID, "kind", t.Kind, "resource", t.Resource, "error", err)
	} else {
		t.Status = database.TaskSuccess
		t.Detail = ""
		slog.Info("tâche terminée", "id", t.ID, "kind", t.Kind, "resource", t.Resource)
	}
	r.save(t)
	if r.OnFinish != nil {
		r.OnFinish(*t)
	}
}

// ─── Attente de pipelines ────────────────────────────────────────────────────

// Statuts de fin d'une pipeline (GitLab CI, GitHub Actions normalisé)
func pipelineDone(status string) (done, ok bool) {
	switch status {
	case "success":
		return true, true
	case "failed", "canceled", "cancelled", "skipped":
		return true, false
	}
	return false, false
}

// PipelineEvent signale l'état d'une pipeline (webhook ou sondage) : les
// tâches qui l'attendent avancent. Une tâche qui attend « la prochaine
// pipeline » du repo (PipelineID vide) s'attache à celle-ci.
func (r *Runner) PipelineEvent(repo, pipelineID, status, webURL string) {
	r.mu.Lock()
	waiting, err := r.waiting()
	var resume []int64
	for i := range waiting {
		t := &waiting[i]
		w := t.Wait
		if w.Repo != repo || (w.PipelineID != "" && w.PipelineID != pipelineID) {
			continue
		}
		if w.PipelineID == pipelineID && w.Status == status {
			continue // déjà connu (webhook puis sondage, ou l'inverse)
		}
		w.PipelineID, w.Status = pipelineID, status
		if webURL != "" {
			w.WebURL = webURL
		}
		t.Detail = fmt.Sprintf("pipeline #%s : %s", pipelineID, status)
		done, ok := pipelineDone(status)
		switch {
		case !done:
			r.save(t)
		case ok:
			t.Wait = nil
			t.Step++
			r.save(t)
			resume = append(resume, t.ID)
		default:
			r.finish(t, fmt.Errorf("pipeline #%s terminée en %s", pipelineID, status))
		}
	}
	r.mu.Unlock()
	if err != nil {
		slog.Warn("tâches : événement de pipeline ignoré", "repo", repo, "error", err)
	}
	for _, id := range resume {
		r.enqueue(id)
	}
}

func (r *Runner) waiting() ([]database.Task, error) {
	unfinished, err := r.store.ListTasks(database.TaskFilter{Unfinished: true, Limit: 1000})
	if err != nil {
		return nil, err
	}
	var waiting []database.Task
	for _, t := range unfinished {
		if t.Wait != nil {
			waiting = append(waiting, t)
		}
	}
	return waiting, nil
}

// poll interroge le provider CI pour les pipelines attendues : rattrape un
// webhook perdu, et suffit seul quand la CI n'envoie pas de webhooks
func (r *Runner) poll(ctx context.Context) {
	ticker := time.NewTicker(r.PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.PollOnce()
		}
	}
}

// PollOnce fait un tour de sondage (exporté pour les tests)
func (r *Runner) PollOnce() {
	r.mu.Lock()
	waiting, err := r.waiting()
	r.mu.Unlock()
	if err != nil {
		slog.Warn("tâches : sondage impossible", "error", err)
		return
	}
	for i := range waiting {
		t := &waiting[i]
		w := t.Wait
		p, err := r.findPipeline(w)
		if err != nil {
			slog.Warn("tâches : sondage de pipeline", "id", t.ID, "repo", w.Repo, "error", err)
		}
		if p != nil {
			r.PipelineEvent(w.Repo, p.ID, p.Status, p.WebURL)
			if done, _ := pipelineDone(p.Status); done {
				continue
			}
		}
		if time.Since(w.Since) > r.WaitTimeout {
			msg := "aucune pipeline n'a démarré"
			if w.PipelineID != "" {
				msg = fmt.Sprintf("pipeline #%s toujours %s", w.PipelineID, w.Status)
			}
			r.mu.Lock()
			if fresh, err := r.store.GetTask(t.ID); err == nil && fresh.Wait != nil && !fresh.Finished() {
				r.finish(fresh, fmt.Errorf("%s après %s", msg, r.WaitTimeout))
			}
			r.mu.Unlock()
		}
	}
}

// findPipeline : la pipeline attendue, ou la plus récente du repo créée après
// le début de l'attente (une minute de marge pour les horloges)
func (r *Runner) findPipeline(w *database.TaskWait) (*provider.Pipeline, error) {
	ci, err := r.providers.CI(w.CIProvider)
	if err != nil {
		return nil, err
	}
	if w.PipelineID != "" {
		return ci.GetPipeline(w.Repo, w.PipelineID)
	}
	list, err := ci.ListPipelines(w.Repo, 5)
	if err != nil {
		return nil, err
	}
	for _, p := range list { // les plus récentes d'abord
		if !p.CreatedAt.Before(w.Since.Add(-time.Minute)) {
			return &p, nil
		}
	}
	return nil, nil
}

// ─── Exec : ce qu'une étape voit de sa tâche ────────────────────────────────

type Exec struct {
	r      *Runner
	t      *database.Task
	result map[string]json.RawMessage
	wait   *database.TaskWait
}

func newExec(r *Runner, t *database.Task) (*Exec, error) {
	result := map[string]json.RawMessage{}
	if len(t.Result) > 0 {
		if err := json.Unmarshal(t.Result, &result); err != nil {
			return nil, fmt.Errorf("résultats de la tâche illisibles: %w", err)
		}
	}
	return &Exec{r: r, t: t, result: result}, nil
}

// Task : copie de la tâche (id, ressource, auteur…)
func (x *Exec) Task() database.Task { return *x.t }

// Params décode les paramètres donnés à Submit
func (x *Exec) Params(v any) error {
	if err := json.Unmarshal(x.t.Params, v); err != nil {
		return fmt.Errorf("paramètres de la tâche illisibles: %w", err)
	}
	return nil
}

// Get lit un résultat enregistré par une étape (ou avant un redémarrage)
func (x *Exec) Get(key string, v any) bool {
	raw, ok := x.result[key]
	return ok && json.Unmarshal(raw, v) == nil
}

// Set enregistre un résultat tout de suite : si Symphony s'arrête ensuite,
// l'étape rejouée le retrouve avec Get et ne refait pas ce qui est fait.
// Les résultats sont renvoyés par l'API : pas de secret.
func (x *Exec) Set(key string, v any) {
	raw, err := json.Marshal(v)
	if err != nil {
		slog.Error("résultat de tâche", "key", key, "error", err)
		return
	}
	x.result[key] = raw
	x.t.Result, _ = json.Marshal(x.result)
	x.r.mu.Lock()
	x.r.save(x.t)
	x.r.mu.Unlock()
}

// Progress décrit l'avancement de l'étape en cours (affiché dans l'UI)
func (x *Exec) Progress(format string, args ...any) {
	detail := fmt.Sprintf(format, args...)
	if detail == x.t.Detail {
		return
	}
	x.t.Detail = detail
	x.r.mu.Lock()
	x.r.save(x.t)
	x.r.mu.Unlock()
}

// WaitPipeline termine l'étape en attente d'une pipeline : la tâche reprend à
// l'étape suivante quand elle réussit, échoue si elle échoue. pipelineID vide :
// la prochaine pipeline du repo créée après since.
func (x *Exec) WaitPipeline(ciProvider, repo, pipelineID, webURL string, since time.Time) {
	x.wait = &database.TaskWait{
		CIProvider: ciProvider,
		Repo:       repo,
		PipelineID: pipelineID,
		WebURL:     webURL,
		Since:      since,
	}
}

// IsNotFound : la tâche n'existe pas
func IsNotFound(err error) bool { return errors.Is(err, database.ErrTaskNotFound) }
