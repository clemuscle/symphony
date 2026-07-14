package api

import (
	"encoding/json"
	"net/http"
	"os"

	"github.com/yourorg/symphony/internal/providers"
)

func (s *Server) setupStatus(w http.ResponseWriter, r *http.Request) {
	configured := s.devMode || s.getProviders() != nil
	respond(w, http.StatusOK, map[string]any{
		"configured": configured,
	})
}

// providerConfigView est la vue non-secrète d'une catégorie de provider,
// utilisée pour préremplir le wizard à la réouverture — jamais de token.
type providerConfigView struct {
	Type          string `json:"type"`
	URL           string `json:"url,omitempty"`
	ConfigRepo    string `json:"config_repo,omitempty"`
	TemplatesRepo string `json:"templates_repo,omitempty"`
	Socket        string `json:"socket,omitempty"`
	HasToken      bool   `json:"has_token"`
}

// setupConfig renvoie les types de driver disponibles par catégorie (pour
// peupler les sélecteurs) et la config non-secrète actuelle (pour
// préremplir le formulaire à la réouverture du wizard). Admin uniquement.
func (s *Server) setupConfig(w http.ResponseWriter, r *http.Request) {
	cfg, err := providers.LoadConfig(s.cfgPath)
	if err != nil {
		cfg = &providers.IntegrationConfig{}
		cfg.ApplyEnvOverrides()
	}
	respond(w, http.StatusOK, map[string]any{
		"types": s.availableTypes,
		"config": map[string]providerConfigView{
			"scm": {
				Type:     cfg.SCM.Type,
				URL:      cfg.SCM.URL,
				HasToken: cfg.SCM.Token != "",
			},
			"ci": {
				Type:          cfg.CI.Type,
				ConfigRepo:    cfg.CI.ConfigRepo,
				TemplatesRepo: cfg.CI.TemplatesRepo,
				HasToken:      cfg.CI.Token != "",
			},
			"registry": {
				Type:     cfg.Registry.Type,
				URL:      cfg.Registry.URL,
				HasToken: cfg.Registry.Token != "",
			},
			"deploy": {
				Type:   cfg.Deploy.Type,
				Socket: cfg.Deploy.Socket,
			},
		},
	})
}

type setupTestRequest struct {
	Category string            `json:"category"`
	Type     string            `json:"type"`
	Config   map[string]string `json:"config"`
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
	msg, err := s.testProvider(req.Category, req.Type, req.Config)
	if err != nil {
		respond(w, http.StatusOK, map[string]any{"ok": false, "error": err.Error()})
		return
	}
	respond(w, http.StatusOK, map[string]any{"ok": true, "message": msg})
}

type providerInput struct {
	Type  string `json:"type"`
	URL   string `json:"url"`
	Token string `json:"token"` // vide = ne pas changer le secret existant
}

type setupSaveRequest struct {
	SCM providerInput `json:"scm"`
	CI  struct {
		Type          string `json:"type"`
		ConfigRepo    string `json:"config_repo"`
		TemplatesRepo string `json:"templates_repo"`
		Token         string `json:"token"`
	} `json:"ci"`
	Registry providerInput `json:"registry"`
	Deploy   struct {
		Type   string `json:"type"`
		Socket string `json:"socket"`
	} `json:"deploy"`
}

func (s *Server) setupSave(w http.ResponseWriter, r *http.Request) {
	var req setupSaveRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}
	if req.SCM.Type == "" || req.CI.Type == "" || req.Registry.Type == "" || req.Deploy.Type == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "type requis pour scm, ci, registry et deploy"})
		return
	}
	if req.SCM.URL == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "scm.url requis"})
		return
	}
	// Un token SCM est requis à la toute première configuration — une fois
	// configuré, un champ vide signifie "ne pas changer" (voir UpsertEnv).
	if !s.devMode && s.getProviders() == nil && req.SCM.Token == "" {
		respond(w, http.StatusBadRequest, map[string]string{"error": "scm.token requis"})
		return
	}
	if req.Deploy.Socket == "" {
		req.Deploy.Socket = "/var/run/docker.sock"
	}

	cfg := &providers.IntegrationConfig{
		SCM: providers.SCMConfig{
			Type: req.SCM.Type,
			URL:  req.SCM.URL,
		},
		CI: providers.CIConfig{
			Type:          req.CI.Type,
			ConfigRepo:    req.CI.ConfigRepo,
			TemplatesRepo: req.CI.TemplatesRepo,
		},
		Registry: providers.RegistryConfig{
			Type: req.Registry.Type,
			URL:  req.Registry.URL,
		},
		Deploy: providers.DeployConfig{
			Type:   req.Deploy.Type,
			Socket: req.Deploy.Socket,
		},
	}

	if err := providers.SaveConfig(s.cfgPath, cfg); err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "sauvegarde: " + err.Error()})
		return
	}

	// Les tokens ne passent jamais par config/integrations.yaml (Token est
	// yaml:"-") — ils vont dans le .env géré par Symphony, et sont posés
	// immédiatement dans l'environnement du process pour que le reload
	// juste en dessous les voie sans redémarrage.
	envValues := map[string]string{
		"GITLAB_TOKEN":            req.SCM.Token,
		"SYMPHONY_CI_TOKEN":       req.CI.Token,
		"SYMPHONY_REGISTRY_TOKEN": req.Registry.Token,
	}
	if err := providers.UpsertEnv(providers.DefaultEnvPath, envValues); err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": "sauvegarde secrets: " + err.Error()})
		return
	}
	for k, v := range envValues {
		if v != "" {
			os.Setenv(k, v)
		}
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
