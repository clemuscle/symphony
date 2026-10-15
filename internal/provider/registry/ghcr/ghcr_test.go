package ghcr

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func fakeGitHub(t *testing.T, calls *[]string) *Provider {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		*calls = append(*calls, r.Method+" "+r.URL.Path)
		switch {
		case r.URL.Path == "/user":
			w.Write([]byte(`{"login":"Clemuscle"}`))
		case r.Method == "GET" && r.URL.Path == "/user/packages/container/api/versions":
			w.Write([]byte(`[
				{"updated_at":"2026-10-01T10:00:00Z","metadata":{"container":{"tags":["abc123"]}}},
				{"updated_at":"2026-10-02T10:00:00Z","metadata":{"container":{"tags":["def456","latest"]}}}
			]`))
		case r.Method == "DELETE" && r.URL.Path == "/orgs/acme/packages/container/web":
			w.WriteHeader(http.StatusNoContent)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)
	return New("ghcr", "", "tok").WithAPIURL(srv.URL)
}

func TestGHCR(t *testing.T) {
	var calls []string
	p := fakeGitHub(t, &calls)

	if got := p.GetImageURL("Clemuscle/API"); got != "ghcr.io/clemuscle/api" {
		t.Errorf("image = %s", got)
	}
	tags, err := p.ListTags("clemuscle/api")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(tags, ",") != "latest,def456,abc123" {
		t.Errorf("tags = %v", tags)
	}
	// Pas encore d'image publiée : liste vide
	if tags, err := p.ListTags("clemuscle/vide"); err != nil || len(tags) != 0 {
		t.Errorf("sans image : %v, %v", tags, err)
	}
	if err := p.DeleteImages("acme/web"); err != nil {
		t.Errorf("suppression : %v", err)
	}
	if err := p.DeleteImages("acme/absent"); err != nil {
		t.Errorf("suppression d'un package absent : %v", err)
	}
	if a := p.Auth(); a == nil || a.Username != "Clemuscle" || a.ServerAddress != "ghcr.io" {
		t.Errorf("auth = %+v", a)
	}
	users := 0
	for _, c := range calls {
		if c == "GET /user" {
			users++
		}
	}
	if users != 1 {
		t.Errorf("%d appels à /user, attendu 1 (cache)", users)
	}
}
