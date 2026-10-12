package webhook

import (
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
	holder := NewHolder(&Settings{Endpoint: "http://symphony/webhooks/gitlab", Secret: "s3cret"})
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
