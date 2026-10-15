package githubactions

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalize(t *testing.T) {
	cases := map[[2]string]string{
		{"queued", ""}:                   "pending",
		{"waiting", ""}:                  "pending",
		{"in_progress", ""}:              "running",
		{"completed", "success"}:         "success",
		{"completed", "failure"}:         "failed",
		{"completed", "timed_out"}:       "failed",
		{"completed", "startup_failure"}: "failed",
		{"completed", "cancelled"}:       "canceled",
		{"completed", "skipped"}:         "skipped",
	}
	for in, want := range cases {
		if got := normalize(in[0], in[1]); got != want {
			t.Errorf("normalize(%s, %s) = %s, attendu %s", in[0], in[1], got, want)
		}
	}
}

// TestTriggerPipeline : id du run renvoyé par l'API (return_run_details), ou
// retrouvé dans la liste des runs quand l'API répond 204
func TestTriggerPipeline(t *testing.T) {
	dispatchWait = 3 * time.Second
	for _, withDetails := range []bool{true, false} {
		var inputs map[string]string
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			switch {
			case r.Method == "POST" && r.URL.Path == "/repos/me/api/actions/workflows/ci.yml/dispatches":
				var body struct {
					Ref     string            `json:"ref"`
					Inputs  map[string]string `json:"inputs"`
					Details bool              `json:"return_run_details"`
				}
				json.NewDecoder(r.Body).Decode(&body)
				inputs = body.Inputs
				if withDetails && body.Details {
					w.Write([]byte(`{"workflow_run_id":42,"html_url":"https://github.com/me/api/actions/runs/42"}`))
					return
				}
				w.WriteHeader(http.StatusNoContent)
			case r.Method == "GET" && r.URL.Path == "/repos/me/api/actions/runs":
				if r.URL.Query().Get("event") != "workflow_dispatch" {
					t.Error("runs non filtrés sur workflow_dispatch")
				}
				created := time.Now().UTC().Format(time.RFC3339)
				w.Write([]byte(`{"workflow_runs":[{"id":43,"status":"queued","html_url":"u","head_branch":"main","created_at":"` + created + `"}]}`))
			default:
				http.NotFound(w, r)
			}
		}))
		p := New("gha", "tok").WithAPIURL(srv.URL)
		pl, err := p.TriggerPipeline("me/api", "main", map[string]string{"SYMPHONY_ACTION": "rollback"})
		srv.Close()
		if err != nil {
			t.Fatal(err)
		}
		want := "43"
		if withDetails {
			want = "42"
		}
		if pl.ID != want || pl.Status != "pending" {
			t.Errorf("détails %v : pipeline %+v, attendu l'id %s", withDetails, pl, want)
		}
		if inputs["SYMPHONY_ACTION"] != "rollback" {
			t.Errorf("entrées = %v", inputs)
		}
	}
}

func TestGetPipeline(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte(`{"id":7,"status":"completed","conclusion":"failure","html_url":"u","head_branch":"main",
			"created_at":"2026-10-15T10:00:00Z","updated_at":"2026-10-15T10:05:00Z"}`))
	}))
	defer srv.Close()
	pl, err := New("gha", "tok").WithAPIURL(srv.URL).GetPipeline("me/api", "7")
	if err != nil {
		t.Fatal(err)
	}
	if pl.ID != "7" || pl.Status != "failed" || pl.FinishedAt == nil || pl.FinishedAt.Sub(pl.CreatedAt) != 5*time.Minute {
		t.Errorf("pipeline = %+v", pl)
	}
}
