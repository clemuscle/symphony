package handlers

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

// TestErrorsHideDetail : le client reçoit l'identifiant de requête, jamais le
// détail de l'erreur (requête SQL, réponse brute d'un provider)
func TestErrorsHideDetail(t *testing.T) {
	cases := []struct {
		name   string
		write  func(w http.ResponseWriter, r *http.Request)
		status int
		prefix string
	}{
		{"interne", func(w http.ResponseWriter, r *http.Request) {
			internalError(w, r, errors.New(`pq: relation "projects" does not exist`))
		}, http.StatusInternalServerError, "erreur interne"},
		{"provider", func(w http.ResponseWriter, r *http.Request) {
			providerError(w, r, "liste des repos", errors.New("gitlab: status 401 — {\"message\":\"invalid token glpat-xxx\"}"))
		}, http.StatusBadGateway, "liste des repos : échec du provider"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			middleware.RequestID(http.HandlerFunc(c.write)).ServeHTTP(rec, httptest.NewRequest("GET", "/api/v1/x", nil))
			var body map[string]string
			json.NewDecoder(rec.Body).Decode(&body)
			if rec.Code != c.status || !strings.HasPrefix(body["error"], c.prefix) || body["request_id"] == "" {
				t.Fatalf("réponse %d %v", rec.Code, body)
			}
			if strings.Contains(body["error"], "pq:") || strings.Contains(body["error"], "glpat") {
				t.Errorf("détail divulgué : %s", body["error"])
			}
			if !strings.Contains(body["error"], body["request_id"]) {
				t.Errorf("identifiant de requête absent du message : %s", body["error"])
			}
		})
	}
}
