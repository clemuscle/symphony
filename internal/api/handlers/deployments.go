package handlers

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"

	"github.com/clemuscle/symphony/internal/database"
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
// déploiement par le runtime, puis attente du démarrage du service
func Deploy(providers *provider.Registry, db *database.DB, runner *tasks.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ProjectName     string            `json:"project_name"`
			Image           string            `json:"image"`
			Tag             string            `json:"tag"`
			Port            int               `json:"port"`
			HostPort        int               `json:"host_port"`
			RuntimeProvider string            `json:"runtime_provider"`
			EnvVars         map[string]string `json:"env_vars"`
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

		// Runtime : demandé, sinon celui du projet (choisi par son golden path),
		// sinon le runtime par défaut
		rtID := req.RuntimeProvider
		if rtID == "" && proj != nil {
			rtID = proj.RTProvider
		}
		rt, err := providers.Runtime(rtID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "runtime provider: "+err.Error())
			return
		}
		if busy(w, r, runner, req.ProjectName) {
			return
		}

		submit(w, r, runner, db, KindDeploy, req.ProjectName, deployParams{
			ProjectName: req.ProjectName,
			Image:       req.Image,
			Tag:         req.Tag,
			Port:        req.Port,
			HostPort:    req.HostPort,
			Runtime:     rt.ID(),
			EnvVars:     req.EnvVars,
		}, map[string]any{
			"image":    req.Image,
			"tag":      req.Tag,
			"provider": rt.ID(),
		})
	}
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

		db.UpdateDeploymentStatus(id, "stopped")
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

// deploymentRuntime : le runtime rtID, si l'utilisateur peut agir sur le
// projet du déploiement id (le déploiement porte le nom du projet)
func deploymentRuntime(w http.ResponseWriter, r *http.Request, providers *provider.Registry, db *database.DB,
	rtID, id string) (provider.RuntimeProvider, bool) {
	rt, err := providers.Runtime(rtID)
	if err != nil {
		respondError(w, http.StatusBadRequest, err.Error())
		return nil, false
	}
	d, err := rt.Status(id)
	if errors.Is(err, provider.ErrNotFound) {
		respondError(w, http.StatusNotFound, "déploiement introuvable : "+id)
		return nil, false
	}
	if err != nil {
		providerError(w, r, "état du déploiement", err)
		return nil, false
	}
	if _, ok := authorizeProject(w, r, db, d.Name); !ok {
		return nil, false
	}
	return rt, true
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
