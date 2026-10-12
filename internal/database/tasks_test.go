package database

import (
	"encoding/json"
	"os"
	"testing"
	"time"
)

// Test sur un vrai PostgreSQL, seulement si SYMPHONY_TEST_DB=1 (avec DB_HOST,
// DB_PORT… vers une base jetable) : les tests unitaires n'en demandent pas.
//
//	docker run -d --name pg-test -e POSTGRES_USER=symphony -e POSTGRES_PASSWORD=symphony123 \
//	  -e POSTGRES_DB=symphony -p 55432:5432 postgres:16-alpine
//	SYMPHONY_TEST_DB=1 DB_PORT=55432 go test ./internal/database/
func testDB(t *testing.T) *DB {
	t.Helper()
	if os.Getenv("SYMPHONY_TEST_DB") != "1" {
		t.Skip("SYMPHONY_TEST_DB=1 pour tester contre PostgreSQL")
	}
	db, err := Connect()
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Migrate(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { db.Close() })
	return db
}

func TestTasksPostgres(t *testing.T) {
	db := testDB(t)
	task := &Task{
		Kind: "create_project", Resource: "pg-test-" + time.Now().Format("150405.000"),
		Status: TaskPending, Steps: []string{"un", "deux"},
		Params: json.RawMessage(`{"name":"x"}`), CreatedBy: "bob",
	}
	if err := db.CreateTask(task); err != nil {
		t.Fatal(err)
	}

	// Mise à jour sans attente (wait NULL), puis avec, puis fin
	task.Status, task.Step, task.Detail = TaskRunning, 1, "création du repo"
	task.Result = json.RawMessage(`{"repo":{"path":"root/x"}}`)
	if err := db.UpdateTask(task); err != nil {
		t.Fatalf("mise à jour sans attente : %v", err)
	}
	task.Wait = &TaskWait{CIProvider: "gitlab-ci", Repo: "root/x", Since: time.Now()}
	if err := db.UpdateTask(task); err != nil {
		t.Fatalf("mise à jour avec attente : %v", err)
	}
	got, err := db.GetTask(task.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Step != 1 || got.Detail != "création du repo" || got.Wait == nil || got.Wait.Repo != "root/x" ||
		string(got.Params) != `{"name": "x"}` || len(got.Steps) != 2 {
		t.Errorf("relu : %+v (params %s, wait %+v)", got, got.Params, got.Wait)
	}
	var result map[string]any
	if json.Unmarshal(got.Result, &result) != nil || result["repo"] == nil {
		t.Errorf("résultat relu : %s", got.Result)
	}

	unfinished, err := db.ListTasks(TaskFilter{Resource: task.Resource, Unfinished: true})
	if err != nil || len(unfinished) != 1 {
		t.Fatalf("non terminées : %v, %v", unfinished, err)
	}

	now := time.Now()
	task.Status, task.Wait, task.FinishedAt = TaskSuccess, nil, &now
	if err := db.UpdateTask(task); err != nil {
		t.Fatal(err)
	}
	if got, _ := db.GetTask(task.ID); got.Wait != nil || !got.Finished() || got.FinishedAt == nil {
		t.Errorf("terminée : %+v", got)
	}
	if unfinished, _ := db.ListTasks(TaskFilter{Resource: task.Resource, Unfinished: true}); len(unfinished) != 0 {
		t.Errorf("tâche finie encore listée en cours")
	}
	if _, err := db.GetTask(-1); err != ErrTaskNotFound {
		t.Errorf("tâche absente : %v", err)
	}
}
