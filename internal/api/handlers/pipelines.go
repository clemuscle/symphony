package handlers

import (
	"encoding/json"
	"net/http"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/go-chi/chi/v5"
)

// TriggerPipeline relance la pipeline d'un projet, sur son repo et avec son
// CI, sans variables : une pipeline paramétrée passe par une action Day-2,
// dont les entrées sont déclarées et validées
func TriggerPipeline(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Project string `json:"project"`
			Ref     string `json:"ref"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "body invalide")
			return
		}
		if req.Project == "" {
			respondError(w, http.StatusBadRequest, "project requis")
			return
		}
		if req.Ref == "" {
			req.Ref = "main"
		}
		proj, ok := authorizeProject(w, r, db, req.Project)
		if !ok {
			return
		}
		if proj == nil || proj.RepoPath == "" {
			respondError(w, http.StatusNotFound, "projet introuvable : "+req.Project)
			return
		}
		ci, err := providers.CI(proj.CIProvider)
		if err != nil {
			respondError(w, http.StatusBadRequest, "ci provider: "+err.Error())
			return
		}

		pipeline, err := ci.TriggerPipeline(proj.RepoPath, req.Ref, nil)
		if err != nil {
			providerError(w, r, "lancement de la pipeline", err)
			return
		}

		db.CreatePipeline(&database.Pipeline{
			ProjectName: proj.RepoPath,
			PipelineID:  pipeline.ID,
			CIProvider:  ci.ID(),
			Type:        "manual",
			Status:      pipeline.Status,
			WebURL:      pipeline.WebURL,
			Ref:         pipeline.Ref,
			TriggeredBy: userID(r),
		})
		audit(db, r, "trigger_pipeline", proj.Name, map[string]any{
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
