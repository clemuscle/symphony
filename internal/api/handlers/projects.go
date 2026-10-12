package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
)

type CreateProjectRequest struct {
	Name            string            `json:"name"`
	Description     string            `json:"description"`
	Namespace       string            `json:"namespace"`
	GoldenPathID    string            `json:"golden_path_id"`
	Port            int               `json:"port"`
	SCMProvider     string            `json:"scm_provider"`
	RuntimeProvider string            `json:"runtime_provider"`
	EnvVars         map[string]string `json:"env_vars"`
}

// projectNameRe : nom de repo et de service (DNS : utilisable tel quel comme
// nom Kubernetes, d'image et d'hôte)
var projectNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// CreateProject valide la demande puis lance la création en tâche (202) :
// repo, squelette, fiche catalogue, webhook et première pipeline
func CreateProject(providers *provider.Registry, db *database.DB, templates *gitops.TemplateLoader, runner *tasks.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req CreateProjectRequest
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "body invalide: "+err.Error())
			return
		}

		req.Name = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(req.Name), " ", "-"))
		if !projectNameRe.MatchString(req.Name) {
			respondError(w, http.StatusBadRequest, "name invalide : minuscules, chiffres et tirets, 63 caractères max.")
			return
		}
		if req.Port == 0 {
			req.Port = 8080
		}

		gp, err := templates.GetGoldenPath(req.GoldenPathID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "golden path introuvable: "+req.GoldenPathID)
			return
		}
		scmID := req.SCMProvider
		if scmID == "" {
			scmID = gp.SCMProvider
		}
		scm, err := providers.SCM(scmID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "scm provider: "+err.Error())
			return
		}
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
		if _, err := db.GetProjectByName(req.Name); err == nil {
			respondError(w, http.StatusConflict, "le projet existe déjà: "+req.Name)
			return
		} else if !errors.Is(err, sql.ErrNoRows) {
			respondError(w, http.StatusInternalServerError, "db: "+err.Error())
			return
		}
		if busy(w, runner, req.Name) {
			return
		}

		submit(w, r, runner, db, KindCreateProject, req.Name, createParams{
			CreateProjectRequest: req,
			SCMProvider:          scm.ID(),
			CIProvider:           ci.ID(),
		}, map[string]any{
			"golden_path": gp.ID,
			"language":    gp.Language,
			"scm":         scm.ID(),
		})
	}
}

// serviceFromProject construit la fiche catalogue d'un projet nouvellement créé
func serviceFromProject(proj *database.Project, gp *gitops.GoldenPath, repo *provider.Repo, ci provider.CIProvider) catalogue.Service {
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

// DeleteProject lance la suppression en tâche et répond 202 : GitLab purge
// le container registry de façon asynchrone, ce qui peut prendre plusieurs
// minutes. Pendant ce temps, ListProjects marque le projet "deleting".
func DeleteProject(db *database.DB, runner *tasks.Runner) http.HandlerFunc {
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
		if busy(w, runner, name) {
			return
		}
		submit(w, r, runner, db, KindDeleteProject, name, deleteParams{
			Name:        proj.Name,
			RepoPath:    proj.RepoPath,
			SCMProvider: proj.SCMProvider,
		}, map[string]any{"repo": proj.RepoPath})
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
		// Suppressions en cours : tâches delete_project non terminées
		active, err := db.ListTasks(database.TaskFilter{Unfinished: true, Limit: 1000})
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		deleting := map[string]bool{}
		for _, t := range active {
			if t.Kind == KindDeleteProject {
				deleting[t.Resource] = true
			}
		}
		for i := range projects {
			projects[i].Deleting = deleting[projects[i].Name]
		}
		respond(w, http.StatusOK, projects)
	}
}

// GetProjectImage renvoie l'image du projet dans son registry et ses tags,
// pour préremplir un déploiement
func GetProjectImage(providers *provider.Registry, db *database.DB) http.HandlerFunc {
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
		// Projets créés avant le registry provider : registry par défaut
		reg, err := providers.GetRegistry(proj.RegProvider)
		if err != nil {
			respondError(w, http.StatusNotFound, "registry provider: "+err.Error())
			return
		}
		tags, err := reg.ListTags(proj.RepoPath)
		if errors.Is(err, provider.ErrNotFound) {
			respondError(w, http.StatusNotFound, "repo introuvable dans le registry: "+proj.RepoPath)
			return
		}
		if err != nil {
			respondError(w, http.StatusBadGateway, "registry: "+err.Error())
			return
		}
		respond(w, http.StatusOK, map[string]any{
			"registry_provider": reg.ID(),
			"image":             reg.GetImageURL(proj.RepoPath),
			"tags":              tags,
			"port":              proj.Port,
			"runtime_provider":  proj.RTProvider,
		})
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

func replacePlaceholders(content, name, description string, gp *gitops.GoldenPath, port int) string {
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
