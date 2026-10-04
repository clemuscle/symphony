package handlers

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/yourorg/symphony/internal/config"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/gitops"
	"github.com/yourorg/symphony/internal/provider"
)

type CreateProjectRequest struct {
	Name          string            `json:"name"`
	Description   string            `json:"description"`
	Namespace     string            `json:"namespace"`
	GoldenPathID  string            `json:"golden_path_id"`
	Port          int               `json:"port"`
	SCMProvider   string            `json:"scm_provider"`
	RuntimeProvider string          `json:"runtime_provider"`
	EnvVars       map[string]string `json:"env_vars"`
}

func CreateProject(providers *provider.Registry, db *database.DB, templates *gitops.TemplateLoader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "body invalide: "+err.Error())
			return
		}

		// Validation
		if req.Name == "" {
			respondError(w, http.StatusBadRequest, "name requis")
			return
		}
		req.Name = strings.ToLower(strings.ReplaceAll(req.Name, " ", "-"))
		if req.Port == 0 {
			req.Port = 8080
		}

		// Récupérer le golden path
		gp, err := templates.GetGoldenPath(req.GoldenPathID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "golden path introuvable: "+req.GoldenPathID)
			return
		}

		// Résoudre le provider SCM
		scmID := req.SCMProvider
		if scmID == "" {
			scmID = gp.SCMProvider
		}
		scm, err := providers.SCM(scmID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "scm provider: "+err.Error())
			return
		}

		// Résoudre le provider CI
		ci, err := providers.CI(gp.CIProvider)
		if err != nil {
			respondError(w, http.StatusBadRequest, "ci provider: "+err.Error())
			return
		}
		_ = ci // utilisé plus tard pour déclencher le premier pipeline

		// 1. Créer le repo
		repo, err := scm.CreateRepo(provider.CreateRepoRequest{
			Name:        req.Name,
			Description: req.Description,
			Namespace:   req.Namespace,
			Private:     true,
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "création repo: "+err.Error())
			return
		}

		// 2. Récupérer les fichiers de scaffold
		scaffoldFiles, err := templates.GetScaffoldFiles(*gp)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "scaffold: "+err.Error())
			return
		}

		// Remplacer les placeholders dans le scaffold
		processedFiles := make(map[string]string, len(scaffoldFiles))
		for path, content := range scaffoldFiles {
			content = replacePlaceholders(content, req.Name, req.Description, gp, req.Port)
			processedFiles[path] = content
		}

		// 3. Pousser le scaffold en un seul commit
		if len(processedFiles) > 0 {
			if err := scm.PushFiles(repo.Path, repo.DefaultBranch, processedFiles,
				"chore: scaffold initial [symphony]"); err != nil {
				respondError(w, http.StatusInternalServerError, "push scaffold: "+err.Error())
				return
			}
		}

		// 4. Pousser le template CI
		ciContent, err := templates.GetCITemplate(*gp)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "ci template: "+err.Error())
			return
		}
		ciContent = replacePlaceholders(ciContent, req.Name, req.Description, gp, req.Port)
		if err := scm.PushFile(repo.Path, repo.DefaultBranch, ".gitlab-ci.yml", ciContent,
			"ci: add pipeline [symphony]"); err != nil {
			respondError(w, http.StatusInternalServerError, "push ci template: "+err.Error())
			return
		}

		// 5. Sauvegarder en DB
		proj := &database.Project{
			Name:        req.Name,
			Description: req.Description,
			Language:    gp.Language,
			Type:        gp.Type,
			Port:        req.Port,
			Namespace:   req.Namespace,
			SCMProvider: scm.ID(),
			CIProvider:  gp.CIProvider,
			RegProvider: gp.RegProvider,
			RTProvider:  req.RuntimeProvider,
			RepoPath:    repo.Path,
			RepoURL:     repo.HTTPCloneURL,
			GoldenPath:  req.GoldenPathID,
			CreatedBy:   "system",
		}
		if err := db.CreateProject(proj); err != nil {
			respondError(w, http.StatusInternalServerError, "db: "+err.Error())
			return
		}

		db.LogAudit("create_project", req.Name, map[string]any{
			"golden_path": req.GoldenPathID,
			"language":    gp.Language,
			"scm":         scm.ID(),
			"repo":        repo.Path,
		}, "system", "")

		respond(w, http.StatusCreated, map[string]any{
			"project":  proj,
			"repo":     repo,
			"message":  fmt.Sprintf("Projet %s créé avec succès", req.Name),
			"pipeline": repo.WebURL + "/-/pipelines",
		})
	}
}

func ListProjects(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projects, err := db.ListProjects()
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if projects == nil {
			projects = []database.Project{}
		}
		respond(w, http.StatusOK, projects)
	}
}

func ListRepos(providers *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		scmID := r.URL.Query().Get("provider")
		scm, err := providers.SCM(scmID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		repos, err := scm.ListRepos()
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respond(w, http.StatusOK, repos)
	}
}

func replacePlaceholders(content, name, description string, gp *config.GoldenPath, port int) string {
	replacer := strings.NewReplacer(
		"{{SERVICE_NAME}}", name,
		"{{SERVICE_DESCRIPTION}}", description,
		"{{LANGUAGE}}", gp.Language,
		"{{TYPE}}", gp.Type,
		"{{PORT}}", fmt.Sprintf("%d", port),
		"{{YEAR}}", fmt.Sprintf("%d", time.Now().Year()),
	)
	return replacer.Replace(content)
}
