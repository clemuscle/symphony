package handlers

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/notify"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/clemuscle/symphony/internal/webhook"
	"github.com/go-chi/chi/v5"
)

// Types de tâches
const (
	KindCreateProject  = "create_project"
	KindDeleteProject  = "delete_project"
	KindDeploy         = "deploy"
	KindActionWebhook  = "action_webhook"
	KindActionPipeline = "action_pipeline"
)

// TaskDeps : ce dont les étapes des tâches ont besoin. Les providers sont
// résolus à chaque étape : la configuration peut changer pendant une tâche.
type TaskDeps struct {
	Providers *provider.Registry
	DB        *database.DB
	Stacks    *gitops.StackLoader
	Syncer    *gitops.Syncer
	Webhooks  *webhook.Holder
	Store     *catalogue.Store // fiches : variables des applications
	Envs      *environments.Holder
	Notify    *notify.Dispatcher // nil : pas de notifications
}

// startupTimeout : délai pour qu'un service déployé soit prêt (pull de
// l'image, synchronisation Argo CD, démarrage des pods)
var startupTimeout = 5 * time.Minute

// RegisterTasks déclare les types de tâches de Symphony
func RegisterTasks(r *tasks.Runner, d TaskDeps) {
	r.Register(tasks.Definition{Kind: KindCreateProject, Steps: []tasks.Step{
		{Label: "Repo et squelette", Run: d.createRepo},
		{Label: "Projet et webhook", Run: d.registerProject},
		{Label: "Première pipeline (image)", Run: d.waitFirstPipeline},
	}})
	r.Register(tasks.Definition{Kind: KindDeploy, Steps: []tasks.Step{
		{Label: "Approbation", Run: d.awaitApproval},
		{Label: "Déploiement", Run: d.deploy},
		{Label: "Démarrage du service", Run: d.waitRunning},
	}})
	r.Register(tasks.Definition{Kind: KindDeleteProject, Steps: []tasks.Step{
		{Label: "Déploiements", Run: d.deleteDeployments},
		{Label: "Repo et images", Run: d.deleteRepo},
		{Label: "Fiche catalogue", Run: d.deleteCatalogue},
		{Label: "Projet", Run: d.deleteRecord},
	}})
	r.Register(tasks.Definition{Kind: KindActionWebhook, Steps: []tasks.Step{
		{Label: "Appel du webhook", Run: d.callActionWebhook},
	}})
	r.Register(tasks.Definition{Kind: KindActionPipeline, Steps: []tasks.Step{
		{Label: "Pipeline", Run: d.runActionPipeline},
	}})

	// Fin de tâche : métriques, et audit des échecs (la demande elle-même est
	// journalisée à la soumission, avec l'adresse de l'appelant)
	r.OnFinish = func(t database.Task) {
		end := time.Now()
		if t.FinishedAt != nil {
			end = *t.FinishedAt
		}
		metrics.TaskFinished(t.Kind, t.Status, end.Sub(t.CreatedAt))
		if t.Status == database.TaskFailed {
			if err := d.DB.LogAudit(t.Kind+"_failed", t.Resource, map[string]any{
				"task_id": t.ID,
				"error":   t.Error,
			}, t.CreatedBy, ""); err != nil {
				slog.Warn("audit", "task", t.ID, "error", err)
			}
		}
		d.notifyFinished(t)
	}
	r.OnApprovalWait = d.notifyApprovalWait
}

// submit soumet une tâche, journalise la demande et répond 202 avec la tâche
func submit(w http.ResponseWriter, r *http.Request, runner *tasks.Runner, db *database.DB,
	kind, resource string, params any, details map[string]any) {
	task, err := runner.Submit(kind, resource, params, userID(r))
	if err != nil {
		internalError(w, r, err)
		return
	}
	if details == nil {
		details = map[string]any{}
	}
	details["task_id"] = task.ID
	audit(db, r, kind, resource, details)
	respond(w, http.StatusAccepted, task)
}

// busy répond 409 si une tâche est déjà en cours sur resource
func busy(w http.ResponseWriter, r *http.Request, runner *tasks.Runner, resource string) bool {
	active, err := runner.Active(resource)
	if err != nil {
		internalError(w, r, err)
		return true
	}
	if active != nil && active.Wait != nil && active.Wait.Approval {
		respondError(w, http.StatusConflict, fmt.Sprintf("%s : tâche #%d en attente d'approbation (la décider ou la retirer d'abord)", resource, active.ID))
		return true
	}
	if active != nil {
		respondError(w, http.StatusConflict, fmt.Sprintf("%s : tâche #%d (%s) déjà en cours", resource, active.ID, active.Kind))
		return true
	}
	return false
}

// ─── API ─────────────────────────────────────────────────────────────────────

// ListTasks : tâches récentes, ?resource= pour un projet ou un service
func ListTasks(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		list, err := db.ListTasks(database.TaskFilter{Resource: r.URL.Query().Get("resource"), Limit: limit})
		if err != nil {
			internalError(w, r, err)
			return
		}
		respond(w, http.StatusOK, list)
	}
}

func GetTask(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "id invalide")
			return
		}
		t, err := db.GetTask(id)
		if tasks.IsNotFound(err) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			internalError(w, r, err)
			return
		}
		respond(w, http.StatusOK, t)
	}
}

// TaskEvents diffuse les changements de tâches en Server-Sent Events
// (événement « task », la tâche en JSON). Un commentaire toutes les 20 s
// garde la connexion ouverte à travers les proxys.
func TaskEvents(hub *tasks.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			respondError(w, http.StatusInternalServerError, "streaming non pris en charge")
			return
		}
		events, unsubscribe := hub.Subscribe()
		defer unsubscribe()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no") // nginx : pas de mise en tampon
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "retry: 3000\n\n")
		flusher.Flush()

		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case t, open := <-events:
				if !open {
					return // abonné trop lent : le navigateur se reconnecte et relit la liste
				}
				data, err := json.Marshal(t)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: task\ndata: %s\n\n", data)
				flusher.Flush()
			case <-ping.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	}
}
