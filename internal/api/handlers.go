package api

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourorg/symphony/internal/catalog"
	"github.com/yourorg/symphony/internal/database"
)



func errSetupRequired() map[string]string {
	return map[string]string{"error": "setup_required", "message": "Configurez les providers via le wizard d'initialisation"}
}

func (s *Server) healthz(w http.ResponseWriter, r *http.Request) {
	respond(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *Server) me(w http.ResponseWriter, r *http.Request) {
	if s.auth == nil {
		respond(w, http.StatusOK, map[string]any{
			"sub": "dev", "email": "dev@localhost", "name": "Dev Mode",
			"groups": []string{"admin"}, "is_admin": true,
		})
		return
	}
	s.auth.MeHandler(w, r)
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
	if err := json.NewDecoder(r.Body).Decode(&inputs); err != nil && err != io.EOF {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	for _, in := range action.Inputs {
		val, ok := inputs[in.ID]
		if !ok {
			continue
		}
		n, isNumber := toInt(val)
		if !isNumber {
			continue
		}
		if in.Min != nil && n < *in.Min {
			respond(w, http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("%s doit être supérieur ou égal à %d (reçu: %d)", in.ID, *in.Min, n),
			})
			return
		}
		if in.Max != nil && n > *in.Max {
			respond(w, http.StatusBadRequest, map[string]string{
				"error": fmt.Sprintf("%s doit être inférieur ou égal à %d (reçu: %d)", in.ID, *in.Max, n),
			})
			return
		}
	}

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
	pvds := s.getProviders()
	if pvds == nil {
		respond(w, http.StatusServiceUnavailable, errSetupRequired())
		return
	}
	repos, err := pvds.SCM.ListRepos()
	if err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	respond(w, http.StatusOK, repos)
}

func (s *Server) triggerPipelineHandler(w http.ResponseWriter, r *http.Request) {
	pvds := s.getProviders()
	if pvds == nil {
		respond(w, http.StatusServiceUnavailable, errSetupRequired())
		return
	}
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
	id, err := pvds.CI.TriggerPipeline(req.ProjectPath, req.Ref, req.Vars)
	if err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
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
	pvds := s.getProviders()
	if pvds == nil {
		respond(w, http.StatusServiceUnavailable, errSetupRequired())
		return
	}
	projectPath := r.URL.Query().Get("project")
	pipelineID := r.URL.Query().Get("id")
	if projectPath == "" || pipelineID == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "project et id requis"})
		return
	}
	status, err := pvds.CI.GetPipelineStatus(projectPath, pipelineID)
	if err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}
	applied, err := s.db.UpdatePipelineStatus(pipelineID, status)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if applied {
		s.db.Log("update_pipeline_status", projectPath, "pipeline "+pipelineID+" -> "+status, "system")
	} else {
		log.Printf("statut ignoré pour pipeline %s (déjà dans un état final): %s", pipelineID, status)
	}
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
	deployments, err := s.db.ListDeployments()
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if deployments == nil {
		deployments = []database.Deployment{}
	}
	respond(w, http.StatusOK, deployments)
}

func (s *Server) stopDeployment(w http.ResponseWriter, r *http.Request) {
	pvds := s.getProviders()
	if pvds == nil {
		respond(w, http.StatusServiceUnavailable, errSetupRequired())
		return
	}
	id := chi.URLParam(r, "id")
	d, err := s.db.GetDeploymentByID(id)
	if err != nil {
		respond(w, http.StatusNotFound, map[string]string{"error": "déploiement introuvable"})
		return
	}
	project, err := s.db.GetProject(d.ProjectName)
	if err != nil {
		respond(w, http.StatusNotFound, map[string]string{"error": "projet introuvable"})
		return
	}

	// Principe #3 : destruction déléguée au pipeline CI (job destroy-deploy)
	pvds.CI.TriggerPipeline(project.RepoPath, "main", map[string]string{
		"DESTROY_DEPLOY": "1",
	})
	s.db.UpdateDeploymentStatus(d.ContainerID, "stopped")
	s.db.Log("stop_deployment", d.ProjectName, "pipeline triggered for destroy", "system")
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

// toInt tente de convertir une valeur décodée depuis JSON (typiquement
// float64 pour un nombre) en int. Retourne false si la valeur n'est pas
// numérique.
func toInt(v any) (int, bool) {
	switch n := v.(type) {
	case float64:
		return int(n), true
	case int:
		return n, true
	case json.Number:
		i, err := n.Int64()
		if err != nil {
			return 0, false
		}
		return int(i), true
	default:
		return 0, false
	}
}

const defaultAllowedOrigin = "http://localhost:5173"

func allowedOrigin() string {
	if origin := os.Getenv("ALLOWED_ORIGIN"); origin != "" {
		return origin
	}
	return defaultAllowedOrigin
}

func corsMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", allowedOrigin())
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
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

func (s *Server) reloadConfig(w http.ResponseWriter, r *http.Request) {
	if s.reload == nil {
		respond(w, http.StatusNotImplemented, map[string]string{"error": "reload non configuré"})
		return
	}
	pvds, err := s.reload()
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	s.setProviders(pvds)
	respond(w, http.StatusOK, map[string]string{"message": "providers rechargés"})
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
