// Package webhook reçoit les événements de pipeline envoyés par GitLab et les
// transmet aux tâches qui les attendent. Les webhooks sont créés par Symphony
// sur les repos qu'il crée, avec un secret partagé vérifié à chaque appel.
package webhook

import (
	"crypto/subtle"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/clemuscle/symphony/internal/metrics"
)

// Settings : configuration courante des webhooks
type Settings struct {
	// Endpoint : URL complète que GitLab appelle ; vide = pas de webhook
	Endpoint string
	Secret   string
}

// Enabled : les repos créés doivent être abonnés
func (s *Settings) Enabled() bool { return s != nil && s.Endpoint != "" && s.Secret != "" }

// Holder porte les réglages, remplacés au rechargement de la configuration
type Holder struct{ p atomic.Pointer[Settings] }

func NewHolder(s *Settings) *Holder {
	h := &Holder{}
	h.Set(s)
	return h
}

func (h *Holder) Set(s *Settings) {
	if s == nil {
		s = &Settings{}
	}
	h.p.Store(s)
}

func (h *Holder) Get() *Settings { return h.p.Load() }

// Sink reçoit les événements de pipeline (tasks.Runner)
type Sink interface {
	PipelineEvent(repo, pipelineID, status, webURL string)
}

// StatusRecorder met à jour l'historique des pipelines (database.DB)
type StatusRecorder interface {
	UpdatePipelineStatus(pipelineID, status string) error
}

const maxBody = 1 << 20 // un événement de pipeline fait quelques Ko

// GitLab traite POST /webhooks/gitlab. Route publique : l'appelant est
// authentifié par le secret (en-tête X-Gitlab-Token), comparé en temps
// constant. Les événements autres que les pipelines sont acquittés et ignorés.
func GitLab(settings *Holder, sink Sink, recorder StatusRecorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event := r.Header.Get("X-Gitlab-Event")
		s := settings.Get()
		if !s.Enabled() {
			metrics.Webhook("gitlab", event, "refused")
			http.Error(w, "webhooks désactivés", http.StatusNotFound)
			return
		}
		if subtle.ConstantTimeCompare([]byte(r.Header.Get("X-Gitlab-Token")), []byte(s.Secret)) != 1 {
			metrics.Webhook("gitlab", event, "refused")
			slog.Warn("webhook GitLab refusé : secret invalide", "remote", r.RemoteAddr)
			http.Error(w, "secret invalide", http.StatusUnauthorized)
			return
		}
		if event != "Pipeline Hook" {
			metrics.Webhook("gitlab", event, "ignored")
			w.WriteHeader(http.StatusNoContent)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			metrics.Webhook("gitlab", event, "error")
			http.Error(w, "lecture impossible", http.StatusBadRequest)
			return
		}
		repo, id, status, webURL, err := parsePipeline(body)
		if err != nil {
			metrics.Webhook("gitlab", event, "error")
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if recorder != nil {
			recorder.UpdatePipelineStatus(id, status)
		}
		sink.PipelineEvent(repo, id, status, webURL)
		metrics.Webhook("gitlab", event, "ok")
		w.WriteHeader(http.StatusNoContent)
	}
}

// parsePipeline extrait d'un événement de pipeline GitLab le repo, l'id, le
// statut et l'URL de la pipeline
func parsePipeline(body []byte) (repo, id, status, webURL string, err error) {
	var ev struct {
		ObjectKind       string `json:"object_kind"`
		ObjectAttributes struct {
			ID     int64  `json:"id"`
			Status string `json:"status"`
			URL    string `json:"url"`
		} `json:"object_attributes"`
		Project struct {
			PathWithNamespace string `json:"path_with_namespace"`
			WebURL            string `json:"web_url"`
		} `json:"project"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return "", "", "", "", fmt.Errorf("événement illisible: %w", err)
	}
	if ev.ObjectKind != "pipeline" || ev.ObjectAttributes.ID == 0 || ev.Project.PathWithNamespace == "" || ev.ObjectAttributes.Status == "" {
		return "", "", "", "", fmt.Errorf("événement de pipeline incomplet")
	}
	id = fmt.Sprint(ev.ObjectAttributes.ID)
	webURL = ev.ObjectAttributes.URL
	if webURL == "" && ev.Project.WebURL != "" {
		webURL = strings.TrimRight(ev.Project.WebURL, "/") + "/-/pipelines/" + id
	}
	return ev.Project.PathWithNamespace, id, ev.ObjectAttributes.Status, webURL, nil
}
