package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/providers"
	"github.com/yourorg/symphony/internal/templates"
)

type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Namespace   string `json:"namespace"`
	Language    string `json:"language"`
	Type        string `json:"type"`
	Port        int    `json:"port"`
}

type stepResult struct {
	Step   string `json:"step"`
	Status string `json:"status"`
	Error  string `json:"error"`
}

// runStep exécute une étape de provisioning, enregistre son résultat dans
// provisioning_steps, et continue quel que soit le résultat (best-effort) —
// un repo déjà créé ne doit jamais être annulé parce qu'une étape suivante a
// échoué. Seules les étapes en échec sont journalisées dans l'audit log, pour
// ne pas multiplier son volume par le nombre d'étapes à chaque création.
func (s *Server) runStep(projectName, step string, fn func() error) stepResult {
	err := fn()
	status := "success"
	errMsg := ""
	if err != nil {
		status = "failed"
		errMsg = err.Error()
		s.db.Log("provisioning_step_failed", projectName, step+": "+errMsg, "system")
	}
	s.db.UpsertProvisioningStep(projectName, step, status, errMsg)
	return stepResult{Step: step, Status: status, Error: errMsg}
}

func (s *Server) createProject(w http.ResponseWriter, r *http.Request) {
	pvds := s.getProviders()
	if pvds == nil {
		respond(w, http.StatusServiceUnavailable, errSetupRequired())
		return
	}
	var req CreateProjectRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.Name == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "name requis"})
		return
	}
	if req.Language == "" {
		req.Language = "go"
	}
	if req.Port == 0 {
		req.Port = 8080
	}

	// 1. Créer le repo — seule étape qui bloque tout le reste si elle échoue.
	repo, err := pvds.SCM.CreateRepo(providers.RepoRequest{
		Name:        req.Name,
		Description: req.Description,
		Namespace:   req.Namespace,
		Private:     true,
	})
	if err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": "scm: " + err.Error()})
		return
	}

	// 2. Persister le projet immédiatement — avant les étapes suivantes, pour
	// qu'un crash en cours de route laisse quand même une trace durable que
	// le repo existe et que le provisioning a été tenté.
	project := &database.Project{
		Name:        req.Name,
		Description: req.Description,
		Language:    req.Language,
		Type:        req.Type,
		Port:        req.Port,
		Namespace:   req.Namespace,
		RepoURL:     repo.HTTPCloneURL,
		RepoPath:    repo.Path,
	}
	if err := s.db.CreateProject(project); err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "db: " + err.Error()})
		return
	}

	// 3. Étapes restantes, séquentielles (scaffold et pipeline écrivent dans le
	// même repo/branche — les paralléliser créerait un risque de race sur la
	// même ref git), chacune best-effort.
	scaffoldVars := templates.ScaffoldVars{
		ServiceName:        req.Name,
		ServiceDescription: req.Description,
		Port:               req.Port,
		Language:           req.Language,
		Type:               req.Type,
		GitServerURL:       pvds.SCMBaseURL,
		ConfigRepoPath:     pvds.CIConfigRepo,
	}
	steps := []stepResult{
		s.runStep(project.Name, "scaffold", func() error {
			files, err := s.tmpl.RenderFiles(req.Language, scaffoldVars)
			if err != nil {
				return err
			}
			for path, content := range files {
				if err := pvds.SCM.PushFile(repo.Path, repo.DefaultBranch, path, content,
					fmt.Sprintf("chore: scaffold %s [symphony]", path)); err != nil {
					return err
				}
			}
			return nil
		}),
		s.runStep(project.Name, "pipeline", func() error {
			content, err := s.tmpl.RenderCI(req.Language, scaffoldVars)
			if err != nil || content == "" {
				return err
			}
			return pvds.SCM.PushFile(repo.Path, repo.DefaultBranch, ".gitlab-ci.yml", content,
				"ci: add pipeline [symphony]")
		}),
		s.runStep(project.Name, "registry", func() error {
			registryURL, err := pvds.Registry.GetRegistryURL(repo.Path)
			if err != nil {
				return err
			}
			project.RegistryURL = registryURL
			return s.db.UpdateProjectRegistryURL(project.Name, registryURL)
		}),
	}

	overallStatus := "ready"
	var failed []string
	for _, st := range steps {
		if st.Status == "failed" {
			overallStatus = "degraded"
			failed = append(failed, st.Step)
		}
	}
	s.db.UpdateProjectStatus(project.Name, overallStatus)
	project.Status = overallStatus

	s.db.Log("create_project", project.Name, fmt.Sprintf("lang=%s type=%s status=%s failed=%s",
		req.Language, req.Type, overallStatus, strings.Join(failed, ",")), "system")

	respond(w, http.StatusCreated, map[string]any{
		"project":   project,
		"repo":      repo,
		"pipelines": repo.WebURL + "/-/pipelines",
		"status":    overallStatus,
		"steps":     steps,
	})
}

func (s *Server) deployProject(w http.ResponseWriter, r *http.Request) {
	pvds := s.getProviders()
	if pvds == nil {
		respond(w, http.StatusServiceUnavailable, errSetupRequired())
		return
	}
	var req struct {
		ProjectName string `json:"project_name"`
		Image       string `json:"image"`
		Port        int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.ProjectName == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "project_name requis"})
		return
	}

	project, err := s.db.GetProject(req.ProjectName)
	if err != nil {
		respond(w, http.StatusNotFound, map[string]string{"error": "projet introuvable"})
		return
	}

	pipelineID, err := pvds.CI.TriggerPipeline(project.RepoPath, "main", nil)
	if err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": "ci: " + err.Error()})
		return
	}

	d := &database.Deployment{
		ProjectName: req.ProjectName,
		PipelineID:  pipelineID,
		Image:       req.Image,
		Port:        req.Port,
		Status:      "pending",
	}
	if err := s.db.CreateDeployment(d); err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "db: " + err.Error()})
		return
	}
	s.db.Log("deploy", req.ProjectName, "pipeline="+pipelineID, "system")

	respond(w, http.StatusAccepted, d)
}

func (s *Server) listProjectSteps(w http.ResponseWriter, r *http.Request) {
	name := chi.URLParam(r, "name")
	steps, err := s.db.ListProvisioningSteps(name)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if steps == nil {
		steps = []database.ProvisioningStep{}
	}
	respond(w, http.StatusOK, steps)
}

func (s *Server) createRecette(w http.ResponseWriter, r *http.Request) {
	pvds := s.getProviders()
	if pvds == nil {
		respond(w, http.StatusServiceUnavailable, errSetupRequired())
		return
	}
	projectName := chi.URLParam(r, "name")
	var req struct {
		RecetteName string `json:"recette_name"`
		Port        int    `json:"port"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.RecetteName == "" || req.Port == 0 {
		respond(w, http.StatusBadRequest, map[string]string{"error": "recette_name et port requis"})
		return
	}

	project, err := s.db.GetProject(projectName)
	if err != nil {
		respond(w, http.StatusNotFound, map[string]string{"error": "projet introuvable"})
		return
	}

	pipelineID, err := pvds.CI.TriggerPipeline(project.RepoPath, "main", map[string]string{
		"RECETTE_NAME": req.RecetteName,
		"RECETTE_PORT": fmt.Sprintf("%d", req.Port),
	})
	if err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": "ci: " + err.Error()})
		return
	}

	d := &database.Deployment{
		ProjectName: projectName,
		PipelineID:  pipelineID,
		Image:       project.RegistryURL,
		Port:        req.Port,
		Status:      "pending",
		RecetteName: req.RecetteName,
		URL:         fmt.Sprintf("http://localhost:%d", req.Port),
	}
	if err := s.db.CreateDeployment(d); err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "db: " + err.Error()})
		return
	}
	s.db.Log("create_recette", projectName, "recette="+req.RecetteName+" port="+fmt.Sprintf("%d", req.Port), "system")

	respond(w, http.StatusCreated, d)
}

func (s *Server) listRecettes(w http.ResponseWriter, r *http.Request) {
	projectName := chi.URLParam(r, "name")
	recettes, err := s.db.ListRecettes(projectName)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if recettes == nil {
		recettes = []database.Deployment{}
	}
	respond(w, http.StatusOK, recettes)
}

func (s *Server) destroyRecette(w http.ResponseWriter, r *http.Request) {
	pvds := s.getProviders()
	if pvds == nil {
		respond(w, http.StatusServiceUnavailable, errSetupRequired())
		return
	}
	projectName := chi.URLParam(r, "name")
	recetteName := chi.URLParam(r, "recette")

	recette, err := s.db.GetRecette(projectName, recetteName)
	if err != nil {
		respond(w, http.StatusNotFound, map[string]string{"error": "recette introuvable"})
		return
	}
	project, err := s.db.GetProject(projectName)
	if err != nil {
		respond(w, http.StatusNotFound, map[string]string{"error": "projet introuvable"})
		return
	}

	// Principe #3 : destruction déléguée au pipeline CI (job destroy-recette)
	if _, err := pvds.CI.TriggerPipeline(project.RepoPath, "main", map[string]string{
		"DESTROY_RECETTE": recetteName,
	}); err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": "ci: " + err.Error()})
		return
	}
	s.db.UpdateDeploymentStatus(recette.PipelineID, "stopped")
	s.db.Log("destroy_recette", projectName, "recette="+recetteName, "system")
	respond(w, http.StatusOK, map[string]string{"status": "stopped"})
}
