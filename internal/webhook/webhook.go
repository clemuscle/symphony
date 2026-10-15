// Package webhook reçoit les événements de pipeline envoyés par GitLab et
// GitHub, et les transmet aux tâches qui les attendent. Les webhooks sont créés
// par Symphony sur les repos qu'il crée, avec un secret partagé vérifié à
// chaque appel (en-tête X-Gitlab-Token, signature X-Hub-Signature-256).
package webhook

import (
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"sync/atomic"

	"github.com/clemuscle/symphony/internal/metrics"
	githubactions "github.com/clemuscle/symphony/internal/provider/ci/githubactions"
)

// Settings : configuration courante des webhooks
type Settings struct {
	// URL : Symphony vu par les forges ; vide = pas de webhook
	URL    string
	Secret string
}

// Enabled : les repos créés doivent être abonnés
func (s *Settings) Enabled() bool { return s != nil && s.URL != "" && s.Secret != "" }

// Endpoint : URL que la forge platform (gitlab, github) appelle
func (s *Settings) Endpoint(platform string) string {
	return strings.TrimRight(s.URL, "/") + "/webhooks/" + platform
}

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

// GitHub traite POST /webhooks/github. Route publique : le corps est signé
// par GitHub avec le secret (HMAC-SHA256, en-tête X-Hub-Signature-256),
// vérifié en temps constant avant toute lecture. Seuls les événements
// workflow_run font avancer les tâches ; ping et les autres sont acquittés.
func GitHub(settings *Holder, sink Sink, recorder StatusRecorder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		event := r.Header.Get("X-GitHub-Event")
		s := settings.Get()
		if !s.Enabled() {
			metrics.Webhook("github", event, "refused")
			http.Error(w, "webhooks désactivés", http.StatusNotFound)
			return
		}
		body, err := io.ReadAll(io.LimitReader(r.Body, maxBody))
		if err != nil {
			metrics.Webhook("github", event, "error")
			http.Error(w, "lecture impossible", http.StatusBadRequest)
			return
		}
		mac := hmac.New(sha256.New, []byte(s.Secret))
		mac.Write(body)
		want := "sha256=" + hex.EncodeToString(mac.Sum(nil))
		if !hmac.Equal([]byte(r.Header.Get("X-Hub-Signature-256")), []byte(want)) {
			metrics.Webhook("github", event, "refused")
			slog.Warn("webhook GitHub refusé : signature invalide", "remote", r.RemoteAddr)
			http.Error(w, "signature invalide", http.StatusUnauthorized)
			return
		}
		if event != "workflow_run" {
			metrics.Webhook("github", event, "ignored")
			w.WriteHeader(http.StatusNoContent)
			return
		}
		repo, id, status, webURL, err := parseWorkflowRun(body)
		if err != nil {
			metrics.Webhook("github", event, "error")
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if recorder != nil {
			recorder.UpdatePipelineStatus(id, status)
		}
		sink.PipelineEvent(repo, id, status, webURL)
		metrics.Webhook("github", event, "ok")
		w.WriteHeader(http.StatusNoContent)
	}
}

// parseWorkflowRun extrait d'un événement workflow_run le repo, l'id du run,
// son statut (ramené à ceux de Symphony) et son URL
func parseWorkflowRun(body []byte) (repo, id, status, webURL string, err error) {
	var ev struct {
		WorkflowRun struct {
			ID         int64  `json:"id"`
			Status     string `json:"status"`
			Conclusion string `json:"conclusion"`
			HTMLURL    string `json:"html_url"`
		} `json:"workflow_run"`
		Repository struct {
			FullName string `json:"full_name"`
		} `json:"repository"`
	}
	if err := json.Unmarshal(body, &ev); err != nil {
		return "", "", "", "", fmt.Errorf("événement illisible: %w", err)
	}
	if ev.WorkflowRun.ID == 0 || ev.Repository.FullName == "" || ev.WorkflowRun.Status == "" {
		return "", "", "", "", fmt.Errorf("événement workflow_run incomplet")
	}
	return ev.Repository.FullName, fmt.Sprint(ev.WorkflowRun.ID),
		githubactions.Normalize(ev.WorkflowRun.Status, ev.WorkflowRun.Conclusion), ev.WorkflowRun.HTMLURL, nil
}
