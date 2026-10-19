package database

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"
)

// Statuts d'une tâche
const (
	TaskPending = "PENDING"
	TaskRunning = "RUNNING"
	TaskSuccess = "SUCCESS"
	TaskFailed  = "FAILED"
)

// Task : opération longue suivie (internal/tasks l'exécute)
type Task struct {
	ID       int64    `json:"id"`
	Kind     string   `json:"kind"`
	Resource string   `json:"resource"`
	Status   string   `json:"status"`
	Steps    []string `json:"steps"`
	Step     int      `json:"step"`
	Detail   string   `json:"detail"`
	// Params n'est jamais renvoyé par l'API : il peut contenir des variables
	// d'environnement de déploiement
	Params     json.RawMessage `json:"-"`
	Result     json.RawMessage `json:"result"`
	Error      string          `json:"error,omitempty"`
	Wait       *TaskWait       `json:"wait,omitempty"`
	CreatedBy  string          `json:"created_by"`
	CreatedAt  time.Time       `json:"created_at"`
	UpdatedAt  time.Time       `json:"updated_at"`
	FinishedAt *time.Time      `json:"finished_at,omitempty"`
}

// TaskWait : pipeline dont la tâche attend la fin, ou accord attendu
// (Approval). PipelineID vide : la prochaine pipeline du repo créée après Since.
type TaskWait struct {
	// Approval : la tâche attend qu'un lead de l'équipe l'approuve ou la refuse
	Approval   bool      `json:"approval,omitempty"`
	CIProvider string    `json:"ci_provider"`
	Repo       string    `json:"repo"`
	PipelineID string    `json:"pipeline_id"`
	Status     string    `json:"status"`
	WebURL     string    `json:"web_url"`
	Since      time.Time `json:"since"`
}

// Finished : la tâche est terminée (succès ou échec)
func (t *Task) Finished() bool { return t.Status == TaskSuccess || t.Status == TaskFailed }

// ErrTaskNotFound : aucune tâche avec cet id
var ErrTaskNotFound = errors.New("tâche introuvable")

const taskColumns = `id, kind, resource, status, steps, step, detail, params, result, error, wait,
	created_by, created_at, updated_at, finished_at`

func scanTask(row interface{ Scan(...any) error }) (*Task, error) {
	var (
		t           Task
		steps, wait []byte
	)
	if err := row.Scan(&t.ID, &t.Kind, &t.Resource, &t.Status, &steps, &t.Step, &t.Detail,
		&t.Params, &t.Result, &t.Error, &wait, &t.CreatedBy, &t.CreatedAt, &t.UpdatedAt, &t.FinishedAt); err != nil {
		return nil, err
	}
	if err := json.Unmarshal(steps, &t.Steps); err != nil {
		return nil, fmt.Errorf("tâche %d: steps: %w", t.ID, err)
	}
	if wait != nil {
		t.Wait = &TaskWait{}
		if err := json.Unmarshal(wait, t.Wait); err != nil {
			return nil, fmt.Errorf("tâche %d: wait: %w", t.ID, err)
		}
	}
	return &t, nil
}

// CreateTask enregistre une tâche (PENDING) et renseigne son id
func (db *DB) CreateTask(t *Task) error {
	steps, _ := json.Marshal(t.Steps)
	return db.QueryRow(`
		INSERT INTO tasks (kind, resource, status, steps, params, created_by)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at, updated_at`,
		t.Kind, t.Resource, t.Status, steps, nullJSON(t.Params, "{}"), t.CreatedBy,
	).Scan(&t.ID, &t.CreatedAt, &t.UpdatedAt)
}

// UpdateTask enregistre l'état d'une tâche (statut, étape, résultats, attente)
func (db *DB) UpdateTask(t *Task) error {
	// NULL sans attente : un []byte nil serait envoyé par lib/pq comme une
	// chaîne vide, refusée par la colonne JSONB
	var wait any
	if t.Wait != nil {
		b, _ := json.Marshal(t.Wait)
		wait = string(b)
	}
	return db.QueryRow(`
		UPDATE tasks SET status=$2, step=$3, detail=$4, result=$5, error=$6, wait=$7,
		       updated_at=NOW(), finished_at=$8
		WHERE id=$1
		RETURNING updated_at`,
		t.ID, t.Status, t.Step, t.Detail, nullJSON(t.Result, "{}"), t.Error, wait, t.FinishedAt,
	).Scan(&t.UpdatedAt)
}

func (db *DB) GetTask(id int64) (*Task, error) {
	t, err := scanTask(db.QueryRow(`SELECT `+taskColumns+` FROM tasks WHERE id=$1`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrTaskNotFound
	}
	return t, err
}

// TaskFilter : resource et status vides = toutes ; Unfinished : PENDING et RUNNING
type TaskFilter struct {
	Resource   string
	Unfinished bool
	Limit      int
}

// ListTasks : les plus récentes d'abord
func (db *DB) ListTasks(f TaskFilter) ([]Task, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	rows, err := db.Query(`SELECT `+taskColumns+` FROM tasks
		WHERE ($1 = '' OR resource = $1)
		  AND (NOT $2 OR status IN ('PENDING', 'RUNNING'))
		ORDER BY created_at DESC, id DESC LIMIT $3`, f.Resource, f.Unfinished, f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	tasks := []Task{}
	for rows.Next() {
		t, err := scanTask(rows)
		if err != nil {
			return nil, err
		}
		tasks = append(tasks, *t)
	}
	return tasks, rows.Err()
}

func nullJSON(v json.RawMessage, empty string) []byte {
	if len(v) == 0 {
		return []byte(empty)
	}
	return v
}
