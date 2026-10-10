package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/yourorg/symphony/internal/catalogue"
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

func CreateProject(providers *provider.Registry, db *database.DB, templates *gitops.TemplateLoader, syncer *gitops.Syncer) http.HandlerFunc {
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
		// Le template CI du golden path est propre à son CI : changer de SCM
		// vers une autre plateforme donnerait un repo dont la CI ne tourne pas
		if err := provider.CheckCompatible(scm, ci); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}

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

		// 3. Ajouter le template CI au scaffold, à l'emplacement attendu par le provider CI
		ciContent, err := templates.GetCITemplate(*gp)
		if err != nil {
			respondError(w, http.StatusInternalServerError, "ci template: "+err.Error())
			return
		}
		processedFiles[ci.ConfigFile()] = replacePlaceholders(ciContent, req.Name, req.Description, gp, req.Port)

		// 4. Pousser scaffold + CI en un seul commit : ce push lance la première
		// pipeline, et sur GitLab un premier commit sans .gitlab-ci.yml
		// déclencherait l'Auto DevOps
		if err := scm.PushFiles(repo.Path, repo.DefaultBranch, processedFiles,
			"chore: scaffold initial [symphony]"); err != nil {
			respondError(w, http.StatusInternalServerError, "push scaffold: "+err.Error())
			return
		}

		// 5. Sauvegarder en DB
		rtProvider := req.RuntimeProvider
		if rtProvider == "" {
			rtProvider = gp.RTProvider
		}

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
			RTProvider:  rtProvider,
			RepoPath:    repo.Path,
			RepoURL:     repo.HTTPCloneURL,
			GoldenPath:  req.GoldenPathID,
			CreatedBy:   "system",
		}
		if err := db.CreateProject(proj); err != nil {
			respondError(w, http.StatusInternalServerError, "db: "+err.Error())
			return
		}

		// 6. Enregistrer le service dans le catalogue. Le projet existe déjà :
		// un échec ici est signalé sans annuler la création.
		catalogueErr := ""
		if err := syncer.Register(serviceFromProject(proj, gp, repo, ci)); err != nil {
			slog.Warn("enregistrement catalogue", "project", req.Name, "error", err)
			catalogueErr = err.Error()
		}

		db.LogAudit("create_project", req.Name, map[string]any{
			"golden_path": req.GoldenPathID,
			"language":    gp.Language,
			"scm":         scm.ID(),
			"repo":        repo.Path,
		}, "system", "")

		resp := map[string]any{
			"project":  proj,
			"repo":     repo,
			"message":  fmt.Sprintf("Projet %s créé avec succès", req.Name),
			"pipeline": ci.PipelinesURL(repo.WebURL),
		}
		if catalogueErr != "" {
			resp["catalogue_error"] = catalogueErr
		}
		respond(w, http.StatusCreated, resp)
	}
}

// serviceFromProject construit la fiche catalogue d'un projet nouvellement créé
func serviceFromProject(proj *database.Project, gp *config.GoldenPath, repo *provider.Repo, ci provider.CIProvider) catalogue.Service {
	return catalogue.Service{
		APIVersion: "symphony/v1",
		Kind:       "Service",
		Metadata: catalogue.Metadata{
			Name:      proj.Name,
			Owner:     proj.CreatedBy,
			Tags:      []string{gp.Language, "symphony"},
			Tier:      "standard",
			Lifecycle: "staging",
			Labels:    map[string]string{"golden_path": gp.ID},
		},
		Spec: catalogue.ServiceSpec{
			Type:     gp.Type,
			Language: gp.Language,
			Repo:     repo.WebURL,
			Links: []catalogue.Link{
				{Title: "Repository", URL: repo.WebURL, Icon: "code"},
				{Title: "Pipelines", URL: ci.PipelinesURL(repo.WebURL), Icon: "ci"},
			},
			Actions: []catalogue.Action{
				{
					Name:        "Relancer la pipeline",
					Description: "Relance la pipeline CI sur main",
					CIProvider:  ci.ID(),
				},
			},
		},
	}
}

// deletingProjects : projets dont la suppression tourne en arrière-plan
var deletingProjects sync.Map

// DeleteProject lance la suppression en arrière-plan et répond 202 : GitLab
// purge le container registry de façon asynchrone, ce qui peut prendre
// plusieurs minutes. Pendant ce temps, ListProjects marque le projet "deleting".
func DeleteProject(providers *provider.Registry, db *database.DB, syncer *gitops.Syncer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		proj, err := db.GetProjectByName(name)
		if errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusNotFound, "projet introuvable: "+name)
			return
		}
		if err != nil {
			respondError(w, http.StatusInternalServerError, "db: "+err.Error())
			return
		}

		// Les projets antérieurs à la migration 002 n'ont pas de scm_provider : provider par défaut
		scm, err := providers.SCM(proj.SCMProvider)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		if _, running := deletingProjects.LoadOrStore(name, true); running {
			respondError(w, http.StatusConflict, "suppression déjà en cours: "+name)
			return
		}

		go func() {
			defer deletingProjects.Delete(name)
			if err := deleteProject(proj, scm, db, syncer); err != nil {
				slog.Error("suppression projet", "project", name, "error", err)
				db.LogAudit("delete_project_failed", name, map[string]any{
					"repo":  proj.RepoPath,
					"error": err.Error(),
				}, "system", "")
				return
			}
			slog.Info("projet supprimé", "project", name)
			db.LogAudit("delete_project", name, map[string]any{
				"scm":  scm.ID(),
				"repo": proj.RepoPath,
			}, "system", "")
		}()

		respond(w, http.StatusAccepted, map[string]string{
			"message": fmt.Sprintf("Suppression de %s en cours", name),
		})
	}
}

// deleteProject supprime le repo (et ses images de registry), la fiche
// catalogue, puis le projet en base. Le repo passe en premier : s'il échoue,
// rien d'autre n'est touché et la suppression peut être relancée. Une
// ressource déjà absente n'est pas une erreur.
func deleteProject(proj *database.Project, scm provider.SCMProvider, db *database.DB, syncer *gitops.Syncer) error {
	if proj.RepoPath != "" {
		if err := scm.DeleteRepo(proj.RepoPath); err != nil && !errors.Is(err, provider.ErrNotFound) {
			return fmt.Errorf("suppression repo: %w", err)
		}
	}
	if err := syncer.Unregister(proj.Name); err != nil {
		return fmt.Errorf("suppression catalogue: %w", err)
	}
	if err := db.DeleteProject(proj.Name, proj.RepoPath); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	return nil
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
		for i := range projects {
			_, projects[i].Deleting = deletingProjects.Load(projects[i].Name)
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
