package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/go-chi/chi/v5"
)

func TriggerPipeline(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			RepoPath   string            `json:"repo_path"`
			Ref        string            `json:"ref"`
			CIProvider string            `json:"ci_provider"`
			Vars       map[string]string `json:"vars"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "body invalide")
			return
		}
		if req.Ref == "" {
			req.Ref = "main"
		}

		ci, err := providers.CI(req.CIProvider)
		if err != nil {
			respondError(w, http.StatusBadRequest, "ci provider: "+err.Error())
			return
		}

		pipeline, err := ci.TriggerPipeline(req.RepoPath, req.Ref, req.Vars)
		if err != nil {
			providerError(w, r, "lancement de la pipeline", err)
			return
		}

		db.CreatePipeline(&database.Pipeline{
			ProjectName: req.RepoPath,
			PipelineID:  pipeline.ID,
			CIProvider:  ci.ID(),
			Type:        "manual",
			Status:      pipeline.Status,
			WebURL:      pipeline.WebURL,
			Ref:         pipeline.Ref,
			TriggeredBy: userID(r),
		})
		audit(db, r, "trigger_pipeline", req.RepoPath, map[string]any{
			"ref":         req.Ref,
			"ci_provider": ci.ID(),
			"pipeline_id": pipeline.ID,
		})

		respond(w, http.StatusOK, pipeline)
	}
}

func GetPipelineStatus(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		repoPath := q.Get("repo")
		pipelineID := q.Get("id")
		ciID := q.Get("provider")

		if repoPath == "" || pipelineID == "" {
			respondError(w, http.StatusBadRequest, "repo et id requis")
			return
		}

		ci, err := providers.CI(ciID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "ci provider: "+err.Error())
			return
		}

		pipeline, err := ci.GetPipeline(repoPath, pipelineID)
		if err != nil {
			providerError(w, r, "état de la pipeline", err)
			return
		}

		db.UpdatePipelineStatus(pipelineID, pipeline.Status)
		respond(w, http.StatusOK, pipeline)
	}
}

func ListPipelines(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// wildcard : le nom de projet contient un "/" (ex. root/mon-projet)
		project := chi.URLParam(r, "*")
		pipelines, err := db.ListPipelines(project, 20)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if pipelines == nil {
			pipelines = []database.Pipeline{}
		}
		respond(w, http.StatusOK, pipelines)
	}
}
