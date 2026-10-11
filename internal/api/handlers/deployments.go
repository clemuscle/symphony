package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/go-chi/chi/v5"
)

func ListDeployments(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		rtID := r.URL.Query().Get("provider")
		rt, err := providers.Runtime(rtID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		deployments, err := rt.List()
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		if deployments == nil {
			deployments = []provider.Deployment{}
		}
		respond(w, http.StatusOK, deployments)
	}
}

func Deploy(providers *provider.Registry, db *database.DB) http.HandlerFunc {
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

		rt, err := providers.Runtime(req.RuntimeProvider)
		if err != nil {
			respondError(w, http.StatusBadRequest, "runtime provider: "+err.Error())
			return
		}

		deployment, err := rt.Deploy(provider.DeployRequest{
			Name:         req.ProjectName,
			Image:        req.Image,
			Tag:          req.Tag,
			Port:         req.Port,
			HostPort:     req.HostPort,
			EnvVars:      req.EnvVars,
			RegistryAuth: registryAuth(providers, db, req.ProjectName, req.Image),
		})
		if err != nil {
			respondError(w, http.StatusInternalServerError, "deploy: "+err.Error())
			return
		}

		db.CreateDeployment(&database.Deployment{
			ProjectName:  req.ProjectName,
			DeploymentID: deployment.ID,
			RTProvider:   rt.ID(),
			Image:        req.Image,
			Tag:          req.Tag,
			Port:         deployment.Port,
			Status:       deployment.Status,
			URL:          deployment.URL,
			DeployedBy:   "system",
		})

		db.LogAudit("deploy", req.ProjectName, map[string]any{
			"image":    req.Image,
			"tag":      req.Tag,
			"provider": rt.ID(),
		}, "system", "")

		respond(w, http.StatusCreated, deployment)
	}
}

func StopDeployment(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rtID := r.URL.Query().Get("provider")

		rt, err := providers.Runtime(rtID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := rt.Stop(id); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		db.UpdateDeploymentStatus(id, "stopped")
		db.LogAudit("stop_deployment", id, nil, "system", "")
		respond(w, http.StatusOK, map[string]string{"status": "stopped", "id": id})
	}
}

func RestartDeployment(providers *provider.Registry, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rtID := r.URL.Query().Get("provider")

		rt, err := providers.Runtime(rtID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}
		if err := rt.Restart(id); err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}

		respond(w, http.StatusOK, map[string]string{"status": "restarted", "id": id})
	}
}

func GetLogs(providers *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id := chi.URLParam(r, "id")
		rtID := r.URL.Query().Get("provider")

		rt, err := providers.Runtime(rtID)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}

		logs, err := rt.Logs(id, 200)
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respond(w, http.StatusOK, map[string]string{"logs": logs, "id": id})
	}
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
