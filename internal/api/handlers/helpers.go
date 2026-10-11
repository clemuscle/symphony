package handlers

import (
	"encoding/json"
	"log/slog"
	"net"
	"net/http"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/database"
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
