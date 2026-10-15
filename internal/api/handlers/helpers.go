package handlers

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/go-chi/chi/v5/middleware"
)

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Error("encode response", "error", err)
	}
}

func respondError(w http.ResponseWriter, code int, msg string) {
	respond(w, code, map[string]string{"error": msg})
}

// internalError : erreur de Symphony lui-même (base, tâches). Le détail va
// dans les logs ; le client ne reçoit que l'identifiant de la requête, pour
// retrouver le détail.
func internalError(w http.ResponseWriter, r *http.Request, err error) {
	id := middleware.GetReqID(r.Context())
	slog.Error("erreur interne", "request_id", id, "method", r.Method, "path", r.URL.Path, "error", err)
	respond(w, http.StatusInternalServerError, map[string]string{
		"error":      "erreur interne (requête " + id + ")",
		"request_id": id,
	})
}

// providerError : un provider (GitLab, Docker…) a échoué. Le client sait
// lequel et quoi ; la réponse brute du provider reste dans les logs.
func providerError(w http.ResponseWriter, r *http.Request, what string, err error) {
	id := middleware.GetReqID(r.Context())
	slog.Error("échec d'un provider", "request_id", id, "method", r.Method, "path", r.URL.Path, "operation", what, "error", err)
	respond(w, http.StatusBadGateway, map[string]string{
		"error":      what + " : échec du provider (requête " + id + ")",
		"request_id": id,
	})
}

// userID : utilisateur de la requête, « system » hors authentification
func userID(r *http.Request) string {
	if u := auth.UserFrom(r.Context()); u != nil {
		return u.ID()
	}
	return "system"
}

// audit journalise une action avec l'utilisateur et l'adresse de la requête
func audit(db *database.DB, r *http.Request, action, resource string, details map[string]any) {
	ip := r.RemoteAddr
	if host, _, err := net.SplitHostPort(ip); err == nil {
		ip = host
	}
	if err := db.LogAudit(action, resource, details, userID(r), ip); err != nil {
		slog.Warn("audit", "action", action, "error", err)
	}
}
