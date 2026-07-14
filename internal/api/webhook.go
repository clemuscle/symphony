package api

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
)

// gitlabPipelineEvent est le sous-ensemble du payload de webhook GitLab pipeline
// dont Symphony a besoin pour mettre à jour ses états internes.
type gitlabPipelineEvent struct {
	ObjectKind       string `json:"object_kind"`
	ObjectAttributes struct {
		ID     int    `json:"id"`
		Status string `json:"status"`
	} `json:"object_attributes"`
	Project struct {
		PathWithNamespace string `json:"path_with_namespace"`
	} `json:"project"`
}

// gitlabWebhook reçoit les événements pipeline GitLab et met à jour
// immédiatement les statuts en DB, sans attendre le cycle de polling 30s.
//
// Route : POST /api/v1/webhooks/gitlab — publique (pas de cookie auth),
// sécurisée par le secret X-Gitlab-Token via GITLAB_WEBHOOK_SECRET. Fail-closed
// si absent : la réconciliation 30s (cmd/symphony/main.go) reste le mécanisme
// authoritative, donc refuser silencieusement le webhook ne casse rien —
// l'alternative (accepter sans authentification) permettrait à n'importe qui
// avec un accès réseau de forger un pipeline_id et corrompre pipelines/deployments.
func (s *Server) gitlabWebhook(w http.ResponseWriter, r *http.Request) {
	secret := os.Getenv("GITLAB_WEBHOOK_SECRET")
	if secret == "" {
		respond(w, http.StatusServiceUnavailable, map[string]string{"error": "webhook non configuré (GITLAB_WEBHOOK_SECRET absent)"})
		return
	}
	if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Gitlab-Token")), []byte(secret)) != 1 {
		respond(w, http.StatusUnauthorized, map[string]string{"error": "invalid token"})
		return
	}

	var event gitlabPipelineEvent
	if err := json.NewDecoder(r.Body).Decode(&event); err != nil {
		respond(w, http.StatusBadRequest, map[string]string{"error": "invalid body"})
		return
	}

	if event.ObjectKind != "pipeline" {
		respond(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	pipelineID := fmt.Sprintf("%d", event.ObjectAttributes.ID)
	gitlabStatus := event.ObjectAttributes.Status

	// Mise à jour du pipeline dans la table pipelines (pour le suivi UI des pipelines manuels)
	if status := mapGitLabStatus(gitlabStatus); status != "" {
		s.db.UpdatePipelineStatus(pipelineID, status)
	}

	// Mise à jour du déploiement associé si ce pipeline est un pipeline de déploiement
	// (la machine d'états ignore l'appel si aucune ligne ne correspond ou si déjà terminal)
	if deployStatus := mapGitLabStatusToDeployment(gitlabStatus); deployStatus != "" {
		s.db.UpdateDeploymentStatus(pipelineID, deployStatus)
	}

	respond(w, http.StatusOK, map[string]string{"status": "ok", "pipeline": pipelineID})
}

// mapGitLabStatus convertit un statut GitLab CI en statut interne Symphony.
// Retourne "" pour les statuts transitoires à ignorer (created, preparing…).
func mapGitLabStatus(s string) string {
	switch s {
	case "running":
		return "running"
	case "success":
		return "success"
	case "failed":
		return "failed"
	case "canceled":
		return "canceled"
	default:
		return ""
	}
}

// mapGitLabStatusToDeployment convertit un statut GitLab CI en statut de déploiement.
// Seuls les statuts terminaux du pipeline ont une signification pour le déploiement :
// success → le container devrait tourner (running), failed/canceled → arrêté.
func mapGitLabStatusToDeployment(s string) string {
	switch s {
	case "success":
		return "running"
	case "failed":
		return "failed"
	case "canceled":
		return "stopped"
	default:
		return ""
	}
}
