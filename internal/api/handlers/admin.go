package handlers

import (
	"errors"
	"net/http"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
)

func ListGoldenPaths(templates *gitops.TemplateLoader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gps := templates.GetGoldenPaths()
		if gps == nil {
			gps = []gitops.GoldenPath{}
		}
		respond(w, http.StatusOK, gps)
	}
}

func ListProviders(providers *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]any{
			"scm":      providers.ListSCM(),
			"ci":       providers.ListCI(),
			"registry": providers.ListRegistries(),
			"runtime":  providers.ListRuntimes(),
			"secrets":  providers.ListSecrets(),
			"identity": providers.ListIdentity(),
		})
	}
}

// ListIdentityGroups : groupes de l'annuaire du provider identity par défaut
// (équipes proposées comme owner d'un projet). supported vaut false si le
// provider n'a pas d'annuaire, ou s'il n'y a pas de provider identity.
func ListIdentityGroups(providers *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idp, err := providers.Identity(r.URL.Query().Get("provider"))
		if err != nil {
			respond(w, http.StatusOK, map[string]any{"supported": false, "groups": []provider.Group{}})
			return
		}
		groups, err := idp.ListGroups()
		if errors.Is(err, provider.ErrUnsupported) {
			respond(w, http.StatusOK, map[string]any{"provider": idp.ID(), "supported": false, "groups": []provider.Group{}})
			return
		}
		if err != nil {
			providerError(w, r, "annuaire "+idp.ID(), err)
			return
		}
		respond(w, http.StatusOK, map[string]any{"provider": idp.ID(), "supported": true, "groups": groups})
	}
}

// ForceSync recharge le catalogue et les golden paths depuis Git
func ForceSync(syncer *gitops.Syncer, templates *gitops.TemplateLoader, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		audit(db, r, "force_sync", "catalogue+templates", nil)
		go syncer.ForceSync()
		go templates.Reload()
		respond(w, http.StatusOK, map[string]string{"status": "sync déclenché"})
	}
}

func ListAudit(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := db.ListAudit(50)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if entries == nil {
			entries = []database.AuditEntry{}
		}
		respond(w, http.StatusOK, entries)
	}
}
