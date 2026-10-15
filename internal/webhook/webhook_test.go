package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type event struct{ repo, id, status, url string }

type fakeSink struct{ events []event }

func (f *fakeSink) PipelineEvent(repo, id, status, url string) {
	f.events = append(f.events, event{repo, id, status, url})
}

type fakeRecorder struct{ updates map[string]string }

func (f *fakeRecorder) UpdatePipelineStatus(id, status string) error {
	f.updates[id] = status
	return nil
}

const pipelineEvent = `{
  "object_kind": "pipeline",
  "object_attributes": {"id": 42, "status": "success", "ref": "main"},
  "project": {"path_with_namespace": "root/hello-api", "web_url": "http://gitlab/root/hello-api"}
}`

func post(h http.Handler, token, event, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest("POST", "/webhooks/gitlab", strings.NewReader(body))
	if token != "" {
		req.Header.Set("X-Gitlab-Token", token)
	}
	req.Header.Set("X-Gitlab-Event", event)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func TestGitLab(t *testing.T) {
	sink := &fakeSink{}
	recorder := &fakeRecorder{updates: map[string]string{}}
	holder := NewHolder(&Settings{URL: "http://symphony", Secret: "s3cret"})
	h := GitLab(holder, sink, recorder)

	cases := []struct {
		name, token, event, body string
		code                     int
	}{
		{"sans secret", "", "Pipeline Hook", pipelineEvent, 401},
		{"mauvais secret", "s3cre", "Pipeline Hook", pipelineEvent, 401},
		{"autre événement", "s3cret", "Push Hook", `{}`, 204},
		{"illisible", "s3cret", "Pipeline Hook", `{`, 400},
		{"incomplet", "s3cret", "Pipeline Hook", `{"object_kind":"pipeline"}`, 400},
		{"pipeline", "s3cret", "Pipeline Hook", pipelineEvent, 204},
	}
	for _, c := range cases {
		if rec := post(h, c.token, c.event, c.body); rec.Code != c.code {
			t.Errorf("%s : code %d, attendu %d (%s)", c.name, rec.Code, c.code, rec.Body)
		}
	}
	want := event{"root/hello-api", "42", "success", "http://gitlab/root/hello-api/-/pipelines/42"}
	if len(sink.events) != 1 || sink.events[0] != want {
		t.Errorf("événements transmis : %+v", sink.events)
	}
	if recorder.updates["42"] != "success" {
		t.Errorf("historique non mis à jour : %v", recorder.updates)
	}

	// Webhooks désactivés (pas de section webhooks) : tout est refusé
	holder.Set(nil)
	if rec := post(h, "", "Pipeline Hook", pipelineEvent); rec.Code != 404 {
		t.Errorf("webhooks désactivés : code %d", rec.Code)
	}
	if len(sink.events) != 1 {
		t.Error("événement transmis alors que les webhooks sont désactivés")
	}
}

func sign(secret, body string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(body))
	return "sha256=" + hex.EncodeToString(mac.Sum(nil))
}

func TestGitHub(t *testing.T) {
	sink := &fakeSink{}
	recorder := &fakeRecorder{updates: map[string]string{}}
	h := GitHub(NewHolder(&Settings{URL: "http://symphony", Secret: "s3cret"}), sink, recorder)
	send := func(signature, event, body string) int {
		req := httptest.NewRequest("POST", "/webhooks/github", strings.NewReader(body))
		req.Header.Set("X-Hub-Signature-256", signature)
		req.Header.Set("X-GitHub-Event", event)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec.Code
	}
	run := `{"action":"completed","workflow_run":{"id":7,"status":"completed","conclusion":"failure","html_url":"https://github.com/me/api/actions/runs/7"},"repository":{"full_name":"me/api"}}`

	if c := send(sign("autre", run), "workflow_run", run); c != http.StatusUnauthorized {
		t.Errorf("mauvaise signature : %d", c)
	}
	if c := send("", "workflow_run", run); c != http.StatusUnauthorized {
		t.Errorf("sans signature : %d", c)
	}
	if c := send(sign("s3cret", `{"zen":"ok"}`), "ping", `{"zen":"ok"}`); c != http.StatusNoContent {
		t.Errorf("ping : %d", c)
	}
	if len(sink.events) != 0 {
		t.Fatalf("événements transmis avant un workflow_run valide : %v", sink.events)
	}
	if c := send(sign("s3cret", run), "workflow_run", run); c != http.StatusNoContent {
		t.Fatalf("workflow_run : %d", c)
	}
	want := event{"me/api", "7", "failed", "https://github.com/me/api/actions/runs/7"}
	if len(sink.events) != 1 || sink.events[0] != want || recorder.updates["7"] != "failed" {
		t.Errorf("événements %v, historique %v", sink.events, recorder.updates)
	}
}
