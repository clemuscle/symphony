package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
)

// ListDeployments liste les déploiements de tous les runtimes (ou du seul
// runtime ?provider=). Un runtime injoignable est journalisé et sauté : les
// autres restent visibles.
func ListDeployments(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ids := providers.ListRuntimes()
		if rtID := r.URL.Query().Get("provider"); rtID != "" {
			ids = []string{rtID}
		}
		deployments := []provider.Deployment{}
		for _, id := range ids {
			rt, err := providers.Runtime(id)
			if err != nil {
				respondError(w, http.StatusBadRequest, err.Error())
				return
			}
			list, err := rt.List()
			if err != nil {
				slog.Warn("liste des déploiements", "runtime", id, "error", err)
				continue
			}
			for _, d := range list {
				d.Provider = rt.ID()
				deployments = append(deployments, d)
			}
		}
		respond(w, http.StatusOK, deployments)
	}
}

// Deploy valide la demande puis lance le déploiement en tâche (202) :
// déploiement par le runtime, puis attente du démarrage du service. Avec des
// environnements, la cible est un environnement (son runtime, celui de sa
// stack si elle en change) ; un environnement protégé ne reçoit que des
// promotions, et un environnement avec approbation fait attendre la demande
// d'un developer jusqu'à l'accord d'un lead de l'équipe.
// Les variables de l'application viennent de sa fiche, pas de la demande.
func Deploy(providers *provider.Registry, db *database.DB, stacks *gitops.StackLoader,
	envs *environments.Holder, runner *tasks.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProjectName     string `json:"project_name"`
			Environment     string `json:"environment"`
			Image           string `json:"image"`
			Tag             string `json:"tag"`
			Port            int    `json:"port"`
			HostPort        int    `json:"host_port"`
			RuntimeProvider string `json:"runtime_provider"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "body invalide")
			return
		}
		if req.ProjectName == "" || req.Image == "" {
			respondError(w, http.StatusBadRequest, "project_name et image requis")
			return
		}
		proj, ok := authorizeProject(w, r, db, req.ProjectName)
		if !ok {
			return
		}

		rtID := req.RuntimeProvider
		approval := false
		if len(envs.List()) > 0 {
			env, _, found := envs.Find(req.Environment)
			switch {
			case !found:
				respondError(w, http.StatusBadRequest, "environment requis : "+envNames(envs))
				return
			case env.Protected:
				from, _ := envs.Previous(env.Name)
				respondError(w, http.StatusForbidden, fmt.Sprintf("%s est protégé : on y promeut une version saine de %s", env.Name, from.Name))
				return
			case proj == nil:
				respondError(w, http.StatusBadRequest, "projet inconnu de Symphony : pas d'environnement")
				return
			}
			rtID, _ = environmentRuntime(envs, stacks, proj, env.Name)
			approval = needsApproval(r, env, proj)
		} else if rtID == "" && proj != nil {
			// Sans environnements : runtime demandé, sinon celui du projet
			// (choisi par sa stack), sinon le runtime par défaut
			rtID = proj.RTProvider
		}
		rt, err := providers.Runtime(rtID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "runtime provider: "+err.Error())
			return
		}
		if n := provider.DeploymentName(req.ProjectName, req.Environment); len(n) > 63 {
			respondError(w, http.StatusBadRequest, "nom de déploiement trop long (63 caractères max.) : "+n)
			return
		}
		if busy(w, r, runner, req.ProjectName) {
			return
		}

		submit(w, r, runner, db, KindDeploy, req.ProjectName, deployParams{
			ProjectName: req.ProjectName,
			Environment: req.Environment,
			Image:       req.Image,
			Tag:         req.Tag,
			Port:        req.Port,
			HostPort:    req.HostPort,
			Runtime:     rt.ID(),
			Origin:      database.OriginDeploy,
			Approval:    approval,
		}, map[string]any{
			"image":       req.Image,
			"tag":         req.Tag,
			"environment": req.Environment,
			"provider":    rt.ID(),
			"approval":    approval,
		})
	}
}

// needsApproval : un déploiement de proj dans env attend l'accord d'un lead.
// Un lead de l'équipe (ou un admin) qui le demande lui-même passe sans attendre.
func needsApproval(r *http.Request, env environments.Environment, proj *database.Project) bool {
	if !env.NeedsApproval() {
		return false
	}
	team := ""
	if proj != nil {
		team = proj.Team
	}
	return !auth.CanApprove(auth.UserFrom(r.Context()), team)
}

// envNames : les environnements, pour les messages d'erreur
func envNames(envs *environments.Holder) string {
	names := make([]string, 0, len(envs.List()))
	for _, e := range envs.List() {
		names = append(names, e.Name)
	}
	return strings.Join(names, ", ")
}

func StopDeployment(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rtID := r.URL.Query().Get("provider")

		rt, ok := deploymentRuntime(w, r, providers, db, rtID, id)
		if !ok {
			return
		}
		if err := rt.Stop(id); err != nil {
			providerError(w, r, "arrêt du déploiement", err)
			return
		}

		db.UpdateDeploymentStatus(id, database.ReleaseStopped)
		audit(db, r, "stop_deployment", id, nil)
		respond(w, http.StatusOK, map[string]string{"status": "stopped", "id": id})
	}
}

func RestartDeployment(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rtID := r.URL.Query().Get("provider")

		rt, ok := deploymentRuntime(w, r, providers, db, rtID, id)
		if !ok {
			return
		}
		if err := rt.Restart(id); err != nil {
			providerError(w, r, "relance du déploiement", err)
			return
		}

		audit(db, r, "restart_deployment", id, nil)
		db.UpdateDeploymentStatus(id, database.ReleaseRunning)
		respond(w, http.StatusOK, map[string]string{"status": "restarted", "id": id})
	}
}

func GetLogs(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rtID := r.URL.Query().Get("provider")

		rt, ok := deploymentRuntime(w, r, providers, db, rtID, id)
		if !ok {
			return
		}

		logs, err := rt.Logs(id, 200)
		if err != nil {
			providerError(w, r, "lecture des logs", err)
			return
		}
		respond(w, http.StatusOK, map[string]string{"logs": logs, "id": id})
	}
}

// deploymentRuntime : le runtime qui porte le déploiement id (rtID, sinon
// celui des runtimes où il existe), si l'utilisateur peut agir sur le projet
// du déploiement
func deploymentRuntime(w http.ResponseWriter, r *http.Request, providers *provider.Registry, db *database.DB,
	rtID, id string) (provider.RuntimeProvider, bool) {
	candidates := providers.ListRuntimes()
	if rtID != "" {
		candidates = []string{rtID}
	}
	var (
		found  []provider.RuntimeProvider
		dep    *provider.Deployment
		failed error
	)
	for _, cid := range candidates {
		rt, err := providers.Runtime(cid)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return nil, false
		}
		d, err := rt.Status(id)
		switch {
		case errors.Is(err, provider.ErrNotFound):
		case err != nil:
			failed = err
		default:
			found, dep = append(found, rt), d
		}
	}
	switch {
	case len(found) > 1:
		respondError(w, http.StatusBadRequest, fmt.Sprintf("%s existe dans plusieurs runtimes : préciser provider", id))
		return nil, false
	case len(found) == 0 && failed != nil:
		providerError(w, r, "état du déploiement", failed)
		return nil, false
	case len(found) == 0:
		respondError(w, http.StatusNotFound, "déploiement introuvable : "+id)
		return nil, false
	}
	project := dep.Project
	if project == "" {
		project = dep.Name
	}
	if _, ok := authorizeProject(w, r, db, project); !ok {
		return nil, false
	}
	return found[0], true
}

// registryAuth : identifiants du registry du projet (ou du registry par
// défaut), transmis seulement si l'image est hébergée sur ce registry, pour
// ne pas envoyer le token à un registry tiers
func registryAuth(providers *provider.Registry, db *database.DB, projectName, image string) *provider.RegistryAuth {
	regID := ""
	if proj, err := db.GetProjectByName(projectName); err == nil {
		regID = proj.RegProvider
	}
	reg, err := providers.GetRegistry(regID)
	if err != nil {
		return nil
	}
	auth := reg.Auth()
	if auth == nil || !strings.HasPrefix(image, auth.ServerAddress+"/") {
		return nil
	}
	return auth
}
