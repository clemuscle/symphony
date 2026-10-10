package handlers

import (
	"net/http"

	"github.com/yourorg/symphony/internal/config"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/provider"
)

func ListGoldenPaths(templates *gitops.TemplateLoader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		gps := templates.GetGoldenPaths()
		if gps == nil {
			gps = []config.GoldenPath{}
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
		})
	}
}

func ForceSync(syncer *gitops.Syncer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		go syncer.ForceSync()
		respond(w, http.StatusOK, map[string]string{"status": "sync déclenché"})
	}
}

func ListAudit(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := db.ListAudit(50)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if entries == nil {
			entries = []database.AuditEntry{}
		}
		respond(w, http.StatusOK, entries)
	}
}
