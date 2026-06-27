package api

import (
	"encoding/json"
	"net/http"

	"github.com/yourorg/symphony/internal/providers"
)

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	configured := s.getProviders() != nil
	respond(w, http.StatusOK, map[string]any{
		"configured": configured,
	})
}

type setupTestRequest struct {
	Type   string            `json:"type"`
	Config map[string]string `json:"config"`
}

func (s *Server) setupTest(w http.ResponseWriter, r *http.Request) {
	var req setupTestRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if s.testProvider == nil {
		respond(w, http.StatusNotImplemented, map[string]string{"error": "test non disponible"})
		return
	}
	msg, err := s.testProvider(req.Type, req.Config)
	if err != nil {
		respond(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	respond(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

type setupSaveRequest struct {
	GitlabURL     string `json:"gitlab_url"`
	GitlabToken   string `json:"gitlab_token"`
	RegistryURL   string `json:"registry_url"`
	ConfigRepo    string `json:"config_repo"`
	TemplatesRepo string `json:"templates_repo"`
	DockerSocket  string `json:"docker_socket"`
}

func (s *Server) setupSave(w http.ResponseWriter, r *http.Request) {
	var req setupSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.GitlabURL == "" || req.GitlabToken == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "gitlab_url et gitlab_token requis"})
		return
	}
	if req.DockerSocket == "" {
		req.DockerSocket = "/var/run/docker.sock"
	}

	cfg := &providers.IntegrationConfig{
		SCM: providers.SCMConfig{
			Type:  "gitlab",
			URL:   req.GitlabURL,
			Token: req.GitlabToken,
		},
		CI: providers.CIConfig{
			Type:          "gitlabci",
			ConfigRepo:    req.ConfigRepo,
			TemplatesRepo: req.TemplatesRepo,
		},
		Registry: providers.RegistryConfig{
			Type: "gitlabregistry",
			URL:  req.RegistryURL,
		},
		Deploy: providers.DeployConfig{
			Type:   "docker",
			Socket: req.DockerSocket,
		},
	}

	if err := providers.SaveConfig(s.cfgPath, cfg); err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "sauvegarde: " + err.Error()})
		return
	}

	if s.reload != nil {
		pvds, err := s.reload()
		if err != nil {
			respond(w, http.StatusInternalServerError, map[string]string{"error": "initialisation providers: " + err.Error()})
			return
		}
		s.setProviders(pvds)
	}

	respond(w, http.StatusOK, map[string]any{"ok": true, "message": "Configuration sauvegardée et providers initialisés"})
}
