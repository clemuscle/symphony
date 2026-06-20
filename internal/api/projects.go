package api

import (
	"encoding/json"
	"net/http"

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

	// 1. Créer le repo
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

	// 2. Scaffold
	s.scm.Scaffold(repo, providers.ScaffoldConfig{
		Name:        req.Name,
		Description: req.Description,
		Language:    req.Language,
		Type:        req.Type,
		Port:        req.Port,
	})

	// 3. Pipelines CI
	for _, pType := range []string{"test", "build"} {
		s.ci.SetupPipeline(repo.Path, providers.PipelineConfig{
			Name:     req.Name,
			Type:     pType,
			Language: req.Language,
		})
	}

	// 4. Registry URL
	registryURL, _ := s.registry.GetRegistryURL(repo.Path)

	// 5. Sauvegarder en DB
	project := &database.Project{
		Name:        req.Name,
		Description: req.Description,
		Language:    req.Language,
		Type:        req.Type,
		Port:        req.Port,
		Namespace:   req.Namespace,
		RepoURL:     repo.HTTPCloneURL,
		RepoPath:    repo.Path,
		RegistryURL: registryURL,
	}
	s.db.CreateProject(project)
	s.db.Log("create_project", req.Name, "lang="+req.Language+" type="+req.Type, "system")

	respond(w, http.StatusCreated, map[string]any{
		"message":      "projet créé",
		"repo":         repo,
		"registry_url": registryURL,
		"pipelines":    repo.WebURL + "/-/pipelines",
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
