package gitlab

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

// GitLab refuse les commits simultanés sur une branche : le faux serveur
// répond 400 si deux écritures se chevauchent dans le même repo
func TestEcrituresSerialiseesParRepo(t *testing.T) {
	var inFlight, overlaps, writes atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if inFlight.Add(1) > 1 {
			overlaps.Add(1)
		}
		defer inFlight.Add(-1)
		time.Sleep(5 * time.Millisecond)
		writes.Add(1)
		switch r.Method {
		case "DELETE":
			w.WriteHeader(204)
		default:
			w.WriteHeader(201)
		}
	}))
	defer srv.Close()

	p := New("gitlab", srv.URL, "token")
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(2)
		go func() {
			defer wg.Done()
			if err := p.DeleteFile("root/symphony-config", "main", "services/a.yaml", "chore"); err != nil {
				t.Error(err)
			}
		}()
		go func() {
			defer wg.Done()
			if err := p.PushFiles("root/symphony-config", "main", map[string]string{"services/b.yaml": "x"}, "feat"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if overlaps.Load() != 0 {
		t.Errorf("%d écritures simultanées dans le même repo", overlaps.Load())
	}
	if writes.Load() != 16 {
		t.Errorf("%d écritures, attendu 16", writes.Load())
	}
}

// Un projet déjà planifié pour suppression : GitLab redirige son ancien chemin
// vers une URL injoignable. DeleteRepo le considère comme absent.
func TestDeleteRepoDejaPlanifie(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, "http://127.0.0.1:1/api/v4/projects/8/registry/repositories", http.StatusMovedPermanently)
	}))
	defer srv.Close()
	if err := New("gitlab", srv.URL, "token").DeleteRepo("root/supprime"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("DeleteRepo = %v, attendu ErrNotFound", err)
	}
}

func TestDeleteFileAbsent(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(400)
		w.Write([]byte(`{"message":"A file with this name doesn't exist"}`))
	}))
	defer srv.Close()
	if err := New("gitlab", srv.URL, "token").DeleteFile("root/c", "main", "services/x.yaml", "chore"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("DeleteFile = %v, attendu ErrNotFound", err)
	}
}

// Suppression différée (GitLab 18) : le premier DELETE ne fait que renommer le
// projet ; DeleteRepo le supprime ensuite pour de bon, sous son nouveau chemin
func TestDeleteRepoDefinitif(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.RequestURI())
		switch r.Method + " " + r.URL.EscapedPath() {
		case "GET /api/v4/projects/root%2Fsvc":
			w.Write([]byte(`{"id":42,"path_with_namespace":"root/svc"}`))
		case "GET /api/v4/projects/root%2Fsvc/registry/repositories":
			w.Write([]byte(`[]`))
		case "DELETE /api/v4/projects/root%2Fsvc":
			w.WriteHeader(202)
		case "GET /api/v4/projects/42":
			w.Write([]byte(`{"id":42,"path_with_namespace":"root/svc-deletion_scheduled-42","marked_for_deletion_on":"2026-10-13"}`))
		case "DELETE /api/v4/projects/42":
			w.WriteHeader(202)
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()
	if err := New("gitlab", srv.URL, "token").DeleteRepo("root/svc"); err != nil {
		t.Fatal(err)
	}
	last := calls[len(calls)-1]
	if want := "DELETE /api/v4/projects/42?permanently_remove=true&full_path=root%2Fsvc-deletion_scheduled-42"; last != want {
		t.Errorf("dernier appel = %s, attendu %s", last, want)
	}
}

func TestCreateRepoNamespace(t *testing.T) {
	var gotNamespace any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == "GET" && r.URL.EscapedPath() == "/api/v4/namespaces/apps%2Fteam-a":
			w.Write([]byte(`{"id": 42, "full_path": "apps/team-a"}`))
		case r.Method == "GET" && strings.HasPrefix(r.URL.Path, "/api/v4/namespaces/"):
			w.WriteHeader(http.StatusNotFound)
		case r.Method == "POST" && r.URL.Path == "/api/v4/projects":
			var body map[string]any
			json.NewDecoder(r.Body).Decode(&body)
			gotNamespace = body["namespace_id"]
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"id": 1, "path_with_namespace": "apps/team-a/svc"}`))
		default:
			t.Errorf("appel inattendu %s %s", r.Method, r.URL)
		}
	}))
	defer srv.Close()

	// Groupe par défaut du provider, sous-groupe compris
	p := New("gitlab", srv.URL, "t").WithNamespace("apps/team-a/")
	if _, err := p.CreateRepo(provider.CreateRepoRequest{Name: "svc"}); err != nil {
		t.Fatal(err)
	}
	if gotNamespace != float64(42) {
		t.Errorf("namespace_id = %v, attendu 42", gotNamespace)
	}

	// Groupe introuvable : erreur, pas de repo créé ailleurs
	gotNamespace = nil
	if _, err := p.CreateRepo(provider.CreateRepoRequest{Name: "svc", Namespace: "inconnu"}); err == nil {
		t.Error("groupe introuvable accepté")
	}
	if gotNamespace != nil {
		t.Error("repo créé malgré un groupe introuvable")
	}
}

// TestEnsureWebhook : crée le webhook, ou met à jour le secret de celui qui
// vise déjà la même URL (rotation)
func TestEnsureWebhook(t *testing.T) {
	var calls []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.EscapedPath())
		switch {
		case r.Method == "GET":
			w.Write([]byte(`[{"id":5,"url":"http://symphony/webhooks/gitlab"}]`))
		case r.Method == "PUT":
			w.Write([]byte(`{}`))
		case r.Method == "POST":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	p := New("gitlab", srv.URL, "token")
	if err := p.EnsureWebhook("apps/api", "http://symphony/webhooks/gitlab", "nouveau"); err != nil {
		t.Fatal(err)
	}
	if err := p.EnsureWebhook("apps/api", "http://autre/webhooks/gitlab", "s"); err != nil {
		t.Fatal(err)
	}
	want := "GET /api/v4/projects/apps%2Fapi/hooks,PUT /api/v4/projects/apps%2Fapi/hooks/5," +
		"GET /api/v4/projects/apps%2Fapi/hooks,POST /api/v4/projects/apps%2Fapi/hooks"
	if strings.Join(calls, ",") != want {
		t.Errorf("appels : %v", calls)
	}
}

func TestCheck(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "bon" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if r.URL.Path == "/api/v4/user" {
			w.Write([]byte(`{"username":"root"}`))
		}
	}))
	defer srv.Close()
	if detail, err := New("gitlab", srv.URL, "bon").Check(); err != nil || detail != "connecté en tant que root sur "+srv.URL {
		t.Errorf("Check = %q, %v", detail, err)
	}
	if _, err := New("gitlab", srv.URL, "expiré").Check(); err == nil || !strings.Contains(err.Error(), "jeton refusé") {
		t.Errorf("jeton refusé attendu, obtenu %v", err)
	}
}

// ListGroupRepos parcourt toutes les pages, sans forks ni repos vides ; un
// groupe absent est ErrNotFound
func TestListGroupRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.EscapedPath() != "/api/v4/groups/apps%2Flegacy/projects" {
			w.WriteHeader(404)
			return
		}
		q := r.URL.Query()
		if q.Get("include_subgroups") != "true" || q.Get("archived") != "false" {
			t.Errorf("paramètres : %s", r.URL.RawQuery)
		}
		var page []map[string]any
		if q.Get("page") == "1" {
			for i := 0; i < 100; i++ {
				page = append(page, map[string]any{"id": i, "path_with_namespace": fmt.Sprintf("apps/legacy/r%d", i), "default_branch": "main"})
			}
		} else {
			page = []map[string]any{
				{"id": 100, "path_with_namespace": "apps/legacy/billing", "default_branch": "master"},
				{"id": 101, "path_with_namespace": "apps/legacy/fork", "default_branch": "main", "forked_from_project": map[string]any{"id": 1}},
				{"id": 102, "path_with_namespace": "apps/legacy/vide"},
				{"id": 103, "path_with_namespace": "apps/legacy/partant", "default_branch": "main", "marked_for_deletion_on": "2026-10-18"},
			}
		}
		json.NewEncoder(w).Encode(page)
	}))
	defer srv.Close()
	p := New("gitlab", srv.URL, "t")
	repos, err := p.ListGroupRepos("/apps/legacy/")
	if err != nil {
		t.Fatal(err)
	}
	if len(repos) != 101 || repos[100].Path != "apps/legacy/billing" || repos[100].DefaultBranch != "master" {
		t.Errorf("%d repos, dernier %+v", len(repos), repos[len(repos)-1])
	}
	if _, err := p.ListGroupRepos("absent"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("groupe absent : %v", err)
	}
}
