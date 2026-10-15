package handlers

import (
	"database/sql"
	"errors"
	"net/http"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/database"
)

// authorizeProject vérifie que l'utilisateur peut agir sur le projet name
// (déployer, lancer une pipeline ou une action) : admin, ou developer membre
// de l'équipe du projet. Répond 403 (ou 500) sinon. Un projet inconnu de
// Symphony est réservé aux admins.
func authorizeProject(w http.ResponseWriter, r *http.Request, db *database.DB, name string) (*database.Project, bool) {
	u := auth.UserFrom(r.Context())
	p, err := db.GetProjectByName(name)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		internalError(w, r, err)
		return nil, false
	}
	if u != nil && u.Role.AtLeast(auth.RoleAdmin) {
		return p, true // p nil : projet inconnu, permis aux admins
	}
	switch {
	case p == nil:
		respondError(w, http.StatusForbidden, "projet inconnu de Symphony : réservé aux admins")
	case p.Team == "":
		respondError(w, http.StatusForbidden, "projet sans équipe : réservé aux admins")
	case !auth.CanOperate(u, p.Team):
		respondError(w, http.StatusForbidden, "projet de l'équipe "+p.Team+" : réservé à ses membres et aux admins")
	default:
		return p, true
	}
	return nil, false
}
