package metrics

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestMiddlewareRoutePattern(t *testing.T) {
	r := chi.NewRouter()
	r.Use(Middleware)
	r.Get("/api/v1/projects/{name}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	for _, name := range []string{"a", "b"} {
		r.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "/api/v1/projects/"+name, nil))
	}
	// Une seule série pour les deux projets : la route est le motif, pas le chemin
	if got := testutil.ToFloat64(httpRequests.WithLabelValues("GET", "/api/v1/projects/{name}", "418")); got != 2 {
		t.Errorf("requêtes = %v, attendu 2", got)
	}
}

func TestObserveProviderCall(t *testing.T) {
	ObserveProviderCall("scm", "gitlab-test", "ListFiles", time.Now(), nil)
	ObserveProviderCall("scm", "gitlab-test", "ListFiles", time.Now(), errors.New("boom"))
	for _, result := range []string{"ok", "error"} {
		if got := testutil.ToFloat64(providerCalls.WithLabelValues("scm", "gitlab-test", "ListFiles", result)); got != 1 {
			t.Errorf("%s = %v, attendu 1", result, got)
		}
	}
}

func TestHandler(t *testing.T) {
	Reload("stacks", "ok")
	rec := httptest.NewRecorder()
	Handler().ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/metrics", nil))
	for _, want := range []string{"symphony_reloads_total", "symphony_last_successful_reload_timestamp_seconds", "go_goroutines"} {
		if !strings.Contains(rec.Body.String(), want) {
			t.Errorf("/metrics ne contient pas %s", want)
		}
	}
}
