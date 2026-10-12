package gitlab

import (
	"errors"
	"net/http"
	"net/http/httptest"
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
