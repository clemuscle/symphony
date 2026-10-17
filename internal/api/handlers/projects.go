package handlers

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"slices"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

type CreateProjectRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Namespace   string `json:"namespace"`
	Stack       string `json:"stack"`
	// Parameters : valeurs des paramètres de la stack (spec.parameters)
	Parameters      map[string]any    `json:"parameters"`
	Port            int               `json:"port"`
	SCMProvider     string            `json:"scm_provider"`
	RuntimeProvider string            `json:"runtime_provider"`
	EnvVars         map[string]string `json:"env_vars"`

	// Équipe propriétaire : ses membres (et les admins) déploient le projet et
	// lancent ses actions. Défaut : la seule équipe du créateur. Reprise dans
	// la fiche catalogue (catalog-info.yaml), modifiable ensuite dans Git.
	Owner string `json:"owner"`

	// Fiche catalogue
	Tier      string `json:"tier"`      // critical, standard, internal
	Lifecycle string `json:"lifecycle"` // production, staging, experimental
}

var (
	validTiers      = []string{"critical", "standard", "internal"}
	validLifecycles = []string{"production", "staging", "experimental", "deprecated"}
)

// projectNameRe : nom de repo et de service (DNS : utilisable tel quel comme
// nom Kubernetes, d'image et d'hôte)
var projectNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// CreateProject valide la demande puis lance la création en tâche (202) :
// repo, squelette, fiche catalogue, webhook et première pipeline
func CreateProject(providers *provider.Registry, db *database.DB, stacks *gitops.StackLoader, envs *environments.Holder, runner *tasks.Runner) http.HandlerFunc {
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
		owner, err := projectTeam(auth.UserFrom(r.Context()), strings.TrimSpace(req.Owner))
		if err != nil {
			respondError(w, http.StatusForbidden, err.Error())
			return
		}
		req.Owner = owner
		if req.Tier == "" {
			req.Tier = "standard"
		}
		if req.Lifecycle == "" {
			req.Lifecycle = "staging"
		}
		if !slices.Contains(validTiers, req.Tier) {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("tier invalide : %s attendu", strings.Join(validTiers, ", ")))
			return
		}
		if !slices.Contains(validLifecycles, req.Lifecycle) {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("lifecycle invalide : %s attendu", strings.Join(validLifecycles, ", ")))
			return
		}

		stack, err := stacks.GetStack(req.Stack)
		if err != nil {
			respondError(w, http.StatusBadRequest, "stack introuvable: "+req.Stack)
			return
		}
		// Défauts, valeurs verrouillées et conditions, validés par le JSON
		// Schema de la stack : la tâche reçoit les valeurs complètes
		if req.Parameters, err = stack.Resolve(req.Parameters); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		scmID := req.SCMProvider
		if scmID == "" {
			scmID = stack.SCMProvider
		}
		scm, err := providers.SCM(scmID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "scm provider: "+err.Error())
			return
		}
		ci, err := providers.CI(stack.CIProvider)
		if err != nil {
			respondError(w, http.StatusBadRequest, "ci provider: "+err.Error())
			return
		}
		// La CI de la stack est propre à son CI : changer de SCM vers une autre
		// plateforme donnerait un repo dont la CI ne tourne pas
		if err := provider.CheckCompatible(scm, ci); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := checkInfraRuntimes(providers, envs, stack, req); err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if _, err := db.GetProjectByName(req.Name); err == nil {
			respondError(w, http.StatusConflict, "le projet existe déjà: "+req.Name)
			return
		} else if !errors.Is(err, sql.ErrNoRows) {
			internalError(w, r, err)
			return
		}
		if busy(w, r, runner, req.Name) {
			return
		}

		submit(w, r, runner, db, KindCreateProject, req.Name, createParams{
			CreateProjectRequest: req,
			SCMProvider:          scm.ID(),
			CIProvider:           ci.ID(),
		}, map[string]any{
			"stack":    stack.ID,
			"language": stack.Language,
			"scm":      scm.ID(),
		})
	}
}

// checkInfraRuntimes : l'infra d'une stack (manifests d'opérateurs) ne
// s'applique que sur Kubernetes ; un projet qui l'aurait est refusé si l'un de
// ses environnements tourne sur un autre runtime, avant de créer son repo
func checkInfraRuntimes(providers *provider.Registry, envs *environments.Holder, stack *gitops.Stack, req CreateProjectRequest) error {
	if stack.InfraKept(req.Parameters) == nil {
		return nil
	}
	targets := map[string]string{} // environnement → runtime
	if list := envs.List(); len(list) > 0 {
		for _, e := range list {
			targets[e.Name] = stack.RuntimeFor(e.Name, e.Runtime)
		}
	} else {
		targets[""] = firstNonEmpty(req.RuntimeProvider, stack.RTProvider)
	}
	for env, id := range targets {
		rt, err := providers.Runtime(id)
		if err != nil {
			return fmt.Errorf("runtime %s : %w", id, err)
		}
		if rt.Type() != "argocd" {
			where := "le runtime " + rt.ID()
			if env != "" {
				where = fmt.Sprintf("l'environnement %s tourne sur %s (%s)", env, rt.ID(), rt.Type())
			}
			return fmt.Errorf("cette stack crée son infra (manifests d'opérateurs) sur Kubernetes, mais %s : choisir une stack sans infra, ou en désactiver l'option", where)
		}
	}
	return nil
}

// projectTeam : l'équipe propriétaire d'un nouveau projet. Un developer
// choisit parmi ses équipes (la seule, par défaut) ; un admin peut nommer
// n'importe quelle équipe. Authentification désactivée : saisie libre.
func projectTeam(u *auth.User, owner string) (string, error) {
	if u == nil {
		return "", errors.New("utilisateur inconnu")
	}
	if u.Role.AtLeast(auth.RoleAdmin) {
		if owner == "" && len(u.Teams) == 1 {
			return u.Teams[0], nil
		}
		if owner == "" {
			return u.Name, nil // anonyme (authentification désactivée) ou admin sans équipe
		}
		return owner, nil
	}
	if owner == "" {
		switch len(u.Teams) {
		case 0:
			return "", errors.New("vous n'êtes membre d'aucune équipe : un projet doit appartenir à une équipe")
		case 1:
			return u.Teams[0], nil
		default:
			return "", fmt.Errorf("owner requis : l'une de vos équipes (%s)", strings.Join(u.Teams, ", "))
		}
	}
	if !u.InTeam(owner) {
		return "", fmt.Errorf("vous n'êtes pas membre de l'équipe %s (vos équipes : %s)", owner, strings.Join(u.Teams, ", "))
	}
	return owner, nil
}

// catalogueFile construit la fiche catalogue d'un nouveau projet, poussée dans
// son repo avec le squelette. Une fiche fournie par la stack (même nom dans
// le skeleton) sert de base : elle peut y déclarer liens, SLO et actions.
// kube : les manifests Kubernetes de la stack créés dans le repo (nil sinon) ;
// infra : son infra, si elle y est.
func catalogueFile(base string, p createParams, stack *gitops.Stack, repo *provider.Repo,
	ci provider.CIProvider, createdBy string, kube *catalogue.Kubernetes, infra *catalogue.Infra) (string, error) {
	svc := catalogue.Service{}
	if base != "" {
		var err error
		if svc, err = gitops.ParseService(p.Name, base); err != nil {
			return "", fmt.Errorf("fiche catalogue de la stack: %w", err)
		}
	}
	svc.APIVersion, svc.Kind, svc.Metadata.Name = "symphony/v1", "Service", p.Name
	m := &svc.Metadata
	m.Owner = firstNonEmpty(p.Owner, m.Owner, createdBy)
	m.Tier = firstNonEmpty(p.Tier, m.Tier)
	m.Lifecycle = firstNonEmpty(p.Lifecycle, m.Lifecycle)
	if !slices.Contains(m.Tags, stack.Language) {
		m.Tags = append(m.Tags, stack.Language)
	}
	if m.Labels == nil {
		m.Labels = map[string]string{}
	}
	m.Labels["stack"] = stack.ID

	sp := &svc.Spec
	sp.Type = firstNonEmpty(sp.Type, stack.Type)
	sp.Language = firstNonEmpty(sp.Language, stack.Language)
	sp.Repo = repo.WebURL
	if sp.Kubernetes == nil {
		sp.Kubernetes = kube
	}
	if sp.Infra == nil {
		sp.Infra = infra
	}
	sp.Links = append([]catalogue.Link{
		{Title: "Repository", URL: repo.WebURL, Icon: "code"},
		{Title: "Pipelines", URL: ci.PipelinesURL(repo.WebURL), Icon: "ci"},
	}, sp.Links...)
	if len(sp.Actions) == 0 {
		sp.Actions = []catalogue.Action{{
			Name:        "Relancer la pipeline",
			Description: "Relance la pipeline CI sur main",
		}}
	}
	// Les actions pipeline de la stack visent le CI du projet
	for i := range sp.Actions {
		if sp.Actions[i].WebhookURL == "" && sp.Actions[i].CIProvider == "" {
			sp.Actions[i].CIProvider = ci.ID()
		}
	}

	var buf bytes.Buffer
	buf.WriteString("# Fiche catalogue du service, lue par Symphony toutes les 15 s.\n" +
		"# À tenir à jour par l'équipe : owner, tier, SLO, liens, dépendances, actions.\n")
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(svc); err != nil {
		return "", fmt.Errorf("fiche catalogue: %w", err)
	}
	return buf.String(), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
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
			internalError(w, r, err)
			return
		}
		if busy(w, r, runner, name) {
			return
		}
		submit(w, r, runner, db, KindDeleteProject, name, deleteParams{
			Name:        proj.Name,
			RepoPath:    proj.RepoPath,
			SCMProvider: proj.SCMProvider,
			RegProvider: proj.RegProvider,
		}, map[string]any{"repo": proj.RepoPath})
	}
}

// projectView : un projet et sa fiche catalogue (absente si son repo n'en a pas)
type projectView struct {
	database.Project
	Service *catalogue.Service `json:"service"`
	// Environnements du projet, avec son runtime dans chacun
	Environments []environments.Environment `json:"environments"`
}

func ListProjects(db *database.DB, store *catalogue.Store, stacks *gitops.StackLoader, envs *environments.Holder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		projects, err := db.ListProjects()
		if err != nil {
			internalError(w, r, err)
			return
		}
		// Suppressions en cours : tâches delete_project non terminées
		active, err := db.ListTasks(database.TaskFilter{Unfinished: true, Limit: 1000})
		if err != nil {
			internalError(w, r, err)
			return
		}
		deleting := map[string]bool{}
		for _, t := range active {
			if t.Kind == KindDeleteProject {
				deleting[t.Resource] = true
			}
		}
		views := make([]projectView, len(projects))
		for i, p := range projects {
			p.Deleting = deleting[p.Name]
			views[i] = projectView{Project: p, Environments: projectEnvironments(envs, stacks, &p)}
			if svc, ok := store.Get(p.Name); ok {
				views[i].Service = &svc
			}
		}
		respond(w, http.StatusOK, views)
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
			internalError(w, r, err)
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
			providerError(w, r, "tags du registry", err)
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
			providerError(w, r, "liste des repos", err)
			return
		}
		respond(w, http.StatusOK, repos)
	}
}

// replacePlaceholders : variables du squelette et de la CI, et
// {{params.<nom>}} pour chaque paramètre de la stack
func replacePlaceholders(content, name, description string, stack *gitops.Stack, port int, params map[string]any) string {
	pairs := []string{
		"{{SERVICE_NAME}}", name,
		"{{SERVICE_DESCRIPTION}}", description,
		"{{LANGUAGE}}", stack.Language,
		"{{TYPE}}", stack.Type,
		"{{PORT}}", fmt.Sprintf("%d", port),
		"{{YEAR}}", fmt.Sprintf("%d", time.Now().Year()),
	}
	return strings.NewReplacer(append(pairs, stack.ParamPlaceholders(params)...)...).Replace(content)
}
