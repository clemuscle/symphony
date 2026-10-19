package handlers

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"
	"time"
	"unicode/utf8"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/notify"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
)

// ─── Approbations ────────────────────────────────────────────────────────────

// Approval : un déploiement qui attend l'accord d'un lead, et ce que
// l'utilisateur de la requête peut en faire
type Approval struct {
	Task database.Task `json:"task"`
	Team string        `json:"team"`
	// CanApprove : lead de l'équipe ou admin ; CanReject : de même, ou le
	// demandeur (il retire sa demande)
	CanApprove bool `json:"can_approve"`
	CanReject  bool `json:"can_reject"`
}

// ListApprovals : les déploiements en attente d'approbation, les plus
// récents d'abord (la cloche des leads dans l'interface)
func ListApprovals(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u := auth.UserFrom(r.Context())
		unfinished, err := db.ListTasks(database.TaskFilter{Unfinished: true, Limit: 1000})
		if err != nil {
			internalError(w, r, err)
			return
		}
		list := []Approval{}
		for _, t := range unfinished {
			if t.Wait == nil || !t.Wait.Approval {
				continue
			}
			team := teamOf(db, t.Resource)
			can := auth.CanApprove(u, team)
			list = append(list, Approval{
				Task:       t,
				Team:       team,
				CanApprove: can,
				CanReject:  can || (u != nil && u.ID() == t.CreatedBy),
			})
		}
		respond(w, http.StatusOK, list)
	}
}

// teamOf : l'équipe du projet name, vide s'il n'existe pas
func teamOf(db *database.DB, name string) string {
	p, err := db.GetProjectByName(name)
	if err != nil {
		if !errors.Is(err, sql.ErrNoRows) {
			slog.Warn("équipe du projet", "project", name, "error", err)
		}
		return ""
	}
	return p.Team
}

// maxComment : longueur maximale du commentaire d'une décision
const maxComment = 500

// DecideTask approuve (approve) ou refuse un déploiement en attente. Un lead
// de l'équipe du projet ou un admin décide ; le demandeur peut retirer sa
// demande (refus).
func DecideTask(db *database.DB, runner *tasks.Runner, notifier *notify.Dispatcher, approve bool) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "id invalide")
			return
		}
		var req struct {
			Comment string `json:"comment"`
		}
		// Corps facultatif : sans commentaire, un POST vide suffit
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil && !errors.Is(err, io.EOF) {
			respondError(w, http.StatusBadRequest, "body invalide")
			return
		}
		if utf8.RuneCountInString(req.Comment) > maxComment {
			respondError(w, http.StatusBadRequest, fmt.Sprintf("commentaire trop long (%d caractères max.)", maxComment))
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
		if t.Finished() || t.Wait == nil || !t.Wait.Approval {
			respondError(w, http.StatusConflict, tasks.ErrNotAwaitingApproval.Error())
			return
		}
		u := auth.UserFrom(r.Context())
		team := teamOf(db, t.Resource)
		allowed := auth.CanApprove(u, team) || (!approve && u != nil && u.ID() == t.CreatedBy)
		if !allowed {
			msg := "approbation réservée aux leads de l'équipe " + team + " et aux admins"
			if team == "" {
				msg = "projet sans équipe : approbation réservée aux admins"
			}
			respondError(w, http.StatusForbidden, msg)
			return
		}
		decided, err := runner.Decide(id, tasks.Decision{Approved: approve, By: userID(r), At: time.Now(), Comment: req.Comment})
		if errors.Is(err, tasks.ErrNotAwaitingApproval) {
			respondError(w, http.StatusConflict, err.Error())
			return
		}
		if err != nil {
			internalError(w, r, err)
			return
		}
		action := "deploy_rejected"
		if approve {
			action = "deploy_approved"
		}
		audit(db, r, action, t.Resource, map[string]any{"task_id": id, "comment": req.Comment})
		notifyDecision(notifier, team, *decided, approve, userID(r), req.Comment)
		respond(w, http.StatusOK, decided)
	}
}

// ─── Notifications des équipes ───────────────────────────────────────────────

// describeDeploy : « <projet> en <env> » et l'image, lus dans le résultat
// « target » de la tâche
func describeDeploy(t database.Task) (where string, target deployTarget) {
	var result struct {
		Target deployTarget `json:"target"`
	}
	_ = json.Unmarshal(t.Result, &result)
	target = result.Target
	where = t.Resource
	if target.Environment != "" {
		where += " en " + target.Environment
	}
	return where, target
}

// verb : ce que fait la tâche de déploiement, selon son origine
func (t deployTarget) verb() string {
	switch t.Origin {
	case database.OriginPromotion:
		return "promotion depuis " + t.PromotedFrom
	case database.OriginRollback:
		return "retour arrière"
	}
	return "déploiement"
}

// projectLink : la page du projet dans Symphony
func projectLink(n *notify.Dispatcher, project string) string {
	return n.Link("/?project=" + url.QueryEscape(project))
}

// notifyApprovalWait : un déploiement attend un lead de l'équipe
func (d TaskDeps) notifyApprovalWait(t database.Task) {
	if d.Notify == nil {
		return
	}
	where, target := describeDeploy(t)
	d.Notify.Send(teamOf(d.DB, t.Resource), provider.Notification{
		Title: "Approbation demandée : " + where,
		Text:  fmt.Sprintf("%s demande %s %s (%s). Un lead de l'équipe l'approuve ou la refuse dans Symphony.", t.CreatedBy, articleFor(target), target.Image, target.verb()),
		URL:   d.Notify.Link("/tasks"),
		Level: provider.NotifyApproval,
	})
}

func articleFor(target deployTarget) string {
	if target.Origin == database.OriginRollback {
		return "le retour à"
	}
	return "le déploiement de"
}

// notifyDecision : un lead a approuvé ou refusé un déploiement
func notifyDecision(n *notify.Dispatcher, team string, t database.Task, approved bool, by, comment string) {
	if n == nil {
		return
	}
	where, target := describeDeploy(t)
	n.Send(team, func() provider.Notification {
		if approved {
			return provider.Notification{
				Title: "Approuvé : " + where,
				Text:  fmt.Sprintf("%s approuve %s (%s) demandé par %s.", by, target.Image, target.verb(), t.CreatedBy),
				URL:   projectLink(n, t.Resource),
				Level: provider.NotifyInfo,
			}
		}
		text := fmt.Sprintf("%s refuse %s (%s) demandé par %s.", by, target.Image, target.verb(), t.CreatedBy)
		if by == t.CreatedBy {
			text = fmt.Sprintf("%s retire sa demande : %s (%s).", by, target.Image, target.verb())
		}
		if comment != "" {
			text += " « " + comment + " »"
		}
		return provider.Notification{Title: "Refusé : " + where, Text: text, URL: projectLink(n, t.Resource), Level: provider.NotifyFailure}
	}())
}

// notifyFinished : fin d'un déploiement (réussi ou non), ou échec d'une autre
// tâche d'un projet. Un refus a déjà été notifié par notifyDecision.
func (d TaskDeps) notifyFinished(t database.Task) {
	if d.Notify == nil {
		return
	}
	var result struct {
		Approval *tasks.Decision `json:"approval"`
	}
	_ = json.Unmarshal(t.Result, &result)
	if result.Approval != nil && !result.Approval.Approved {
		return
	}
	team := teamOf(d.DB, t.Resource)
	link := projectLink(d.Notify, t.Resource)
	if t.Kind == KindDeploy {
		where, target := describeDeploy(t)
		if t.Status == database.TaskSuccess {
			d.Notify.Send(team, provider.Notification{
				Title: "Déployé : " + where,
				Text:  fmt.Sprintf("%s (%s) par %s.", target.Image, target.verb(), t.CreatedBy),
				URL:   link,
				Level: provider.NotifySuccess,
			})
			return
		}
		d.Notify.Send(team, provider.Notification{
			Title: "Échec du déploiement : " + where,
			Text:  fmt.Sprintf("%s (%s) par %s : %s", target.Image, target.verb(), t.CreatedBy, t.Error),
			URL:   link,
			Level: provider.NotifyFailure,
		})
		return
	}
	if t.Status == database.TaskFailed && team != "" {
		d.Notify.Send(team, provider.Notification{
			Title: fmt.Sprintf("Échec : %s de %s", taskLabel(t.Kind), t.Resource),
			Text:  fmt.Sprintf("Demandé par %s : %s", t.CreatedBy, t.Error),
			URL:   d.Notify.Link("/tasks"),
			Level: provider.NotifyFailure,
		})
	}
}

// taskLabel : le type de tâche, lisible
func taskLabel(kind string) string {
	switch kind {
	case KindCreateProject:
		return "création"
	case KindDeleteProject:
		return "suppression"
	case KindActionWebhook, KindActionPipeline:
		return "action"
	}
	return kind
}
