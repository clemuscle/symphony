package handlers

import (
	"bufio"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/tasks"
)

func TestTaskEvents(t *testing.T) {
	hub := tasks.NewHub()
	srv := httptest.NewServer(TaskEvents(hub))
	defer srv.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, _ := http.NewRequestWithContext(ctx, "GET", srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("Content-Type = %q", ct)
	}

	lines := bufio.NewScanner(resp.Body)
	lines.Scan() // retry: 3000
	// Abonné dès l'en-tête envoyé : on peut publier
	hub.Publish(database.Task{ID: 7, Kind: "deploy", Status: database.TaskRunning, Params: json.RawMessage(`{"env_vars":{"TOKEN":"secret"}}`)})

	var event, data string
	for lines.Scan() {
		line := lines.Text()
		switch {
		case strings.HasPrefix(line, "event: "):
			event = strings.TrimPrefix(line, "event: ")
		case strings.HasPrefix(line, "data: "):
			data = strings.TrimPrefix(line, "data: ")
		}
		if data != "" {
			break
		}
	}
	if event != "task" {
		t.Errorf("événement %q", event)
	}
	var got map[string]any
	if err := json.Unmarshal([]byte(data), &got); err != nil {
		t.Fatalf("data illisible : %s", data)
	}
	if got["id"] != float64(7) || got["status"] != "RUNNING" {
		t.Errorf("tâche reçue : %v", got)
	}
	if strings.Contains(data, "secret") {
		t.Error("les paramètres de la tâche (variables d'environnement) sont diffusés")
	}
}
