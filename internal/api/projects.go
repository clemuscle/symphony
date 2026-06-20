package api

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/providers"
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
	repo, err := s.scm.CreateRepo(providers.RepoRequest{
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

	// 3. Étapes restantes, séquentielles (scaffold et setup pipeline écrivent
	// tous les deux dans le même repo/branche — les paralléliser créerait un
	// vrai risque de race sur la même ref git), chacune best-effort.
	steps := []stepResult{
		s.runStep(project.Name, "scaffold", func() error {
			return s.scm.Scaffold(repo, providers.ScaffoldConfig{
				Name:        req.Name,
				Description: req.Description,
				Language:    req.Language,
				Type:        req.Type,
				Port:        req.Port,
			})
		}),
		s.runStep(project.Name, "pipeline_test", func() error {
			return s.ci.SetupPipeline(repo.Path, providers.PipelineConfig{
				Name:     req.Name,
				Type:     "test",
				Language: req.Language,
			})
		}),
		s.runStep(project.Name, "pipeline_build", func() error {
			return s.ci.SetupPipeline(repo.Path, providers.PipelineConfig{
				Name:     req.Name,
				Type:     "build",
				Language: req.Language,
			})
		}),
		s.runStep(project.Name, "registry", func() error {
			registryURL, err := s.registry.GetRegistryURL(repo.Path)
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
	var req struct {
		ProjectName string            `json:"project_name"`
		Image       string            `json:"image"`
		Port        int               `json:"port"`
		EnvVars     map[string]string `json:"env_vars"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	result, err := s.deploy.Deploy(providers.DeployRequest{
		ProjectName: req.ProjectName,
		Image:       req.Image,
		Port:        req.Port,
		EnvVars:     req.EnvVars,
	})
	if err != nil {
		respond(w, http.StatusBadGateway, map[string]string{"error": err.Error()})
		return
	}

	s.db.CreateDeployment(&database.Deployment{
		ProjectName: req.ProjectName,
		ContainerID: result.DeploymentID,
		Image:       req.Image,
		Port:        req.Port,
		Status:      "running",
		URL:         result.URL,
	})
	s.db.Log("deploy", req.ProjectName, "image="+req.Image, "system")

	respond(w, http.StatusCreated, result)
}
