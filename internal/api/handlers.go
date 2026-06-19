package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/providers"
)



func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) listServices(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, s.store.All())
}

func (s *Server) getService(w http.ResponseWriter, r *http.Request) {
	svc, ok := s.store.Get(chi.URLParam(r, "name"))
	if !ok {
		respond(w, http.StatusNotFound, map[string]string{"error": "not found"})
		return
	}
	respond(w, http.StatusOK, svc)
}

func (s *Server) triggerAction(w http.ResponseWriter, r *http.Request) {
	svcName := chi.URLParam(r, "name")
	actionName := chi.URLParam(r, "action")

	svc, ok := s.store.Get(svcName)
	if !ok {
		respond(w, http.StatusNotFound, map[string]string{"error": "service not found"})
		return
	}

	var action *catalog.Action
	for _, a := range svc.Spec.Actions {
		if a.Name == actionName {
			action = &a
			break
		}
	}
	if action == nil {
		respond(w, http.StatusNotFound, map[string]string{"error": "action not found"})
		return
	}
	if action.WebhookURL == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "no webhook_url configured"})
		return
	}

	var inputs map[string]any
	json.NewDecoder(r.Body).Decode(&inputs)

	payload := map[string]any{
		"service":   svcName,
		"action":    actionName,
		"inputs":    inputs,
		"triggered": time.Now().UTC().Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)
	resp, err := http.Post(action.WebhookURL, "application/json", bytes.NewReader(body))
	if err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("webhook: %v", err)})
		return
	}
	defer resp.Body.Close()

	s.db.Log("trigger_action", svcName+"/"+actionName, fmt.Sprintf("%v", inputs), "system")
	respond(w, http.StatusOK, map[string]any{"status": "dispatched", "payload": payload})
}

func (s *Server) listProjects(w http.ResponseWriter, r *http.Request) {
	projects, err := s.db.ListProjects()
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if projects == nil {
		projects = []database.Project{}
	}
	respond(w, http.StatusOK, projects)
}

func (s *Server) listRepos(w http.ResponseWriter, r *http.Request) {
	repos, err := s.scm.ListRepos()
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respond(w, http.StatusOK, repos)
}

func (s *Server) triggerPipelineHandler(w http.ResponseWriter, r *http.Request) {
	var req struct {
		ProjectPath string            `json:"project_path"`
		Ref         string            `json:"ref"`
		Vars        map[string]string `json:"vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Ref == "" {
		req.Ref = "main"
	}
	id, err := s.ci.TriggerPipeline(req.ProjectPath, req.Ref, req.Vars)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	s.db.CreatePipeline(&database.Pipeline{
		ProjectName: req.ProjectPath,
		PipelineID:  id,
		Type:        "manual",
		Status:      "pending",
		TriggeredBy: "symphony-ui",
	})
	s.db.Log("trigger_pipeline", req.ProjectPath, "pipeline "+id, "system")

	respond(w, http.StatusOK, map[string]string{"pipeline_id": id})
}

func (s *Server) getPipelineStatusHandler(w http.ResponseWriter, r *http.Request) {
	projectPath := r.URL.Query().Get("project")
	pipelineID := r.URL.Query().Get("id")
	if projectPath == "" || pipelineID == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "project et id requis"})
		return
	}
	status, err := s.ci.GetPipelineStatus(projectPath, pipelineID)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.db.UpdatePipelineStatus(pipelineID, status)
	respond(w, http.StatusOK, map[string]string{"status": status})
}

func (s *Server) listPipelinesHandler(w http.ResponseWriter, r *http.Request) {
	project := chi.URLParam(r, "project")
	pipelines, err := s.db.ListPipelines(project)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if pipelines == nil {
		pipelines = []database.Pipeline{}
	}
	respond(w, http.StatusOK, pipelines)
}

func (s *Server) listDeployments(w http.ResponseWriter, r *http.Request) {
	deployments, err := s.deploy.List()
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if deployments == nil {
		deployments = []providers.Deployment{}
	}
	respond(w, http.StatusOK, deployments)
}

func (s *Server) stopDeployment(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.deploy.Stop(id); err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.db.UpdateDeploymentStatus(id, "stopped")
	s.db.Log("stop_deployment", id, "", "system")
	respond(w, http.StatusOK, map[string]string{"status": "stopped"})
}

func (s *Server) listAudit(w http.ResponseWriter, r *http.Request) {
	entries, err := s.db.ListAudit(50)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if entries == nil {
		entries = []database.AuditEntry{}
	}
	respond(w, http.StatusOK, entries)
}

func respond(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(v)
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) listGoldenPaths(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, s.tmpl.GetGoldenPaths())
}

func (s *Server) reloadTemplates(w http.ResponseWriter, r *http.Request) {
	if err := s.tmpl.Reload(); err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	respond(w, http.StatusOK, map[string]any{
		"golden_paths": len(s.tmpl.GetGoldenPaths()),
		"message":      "templates rechargés",
	})
}
