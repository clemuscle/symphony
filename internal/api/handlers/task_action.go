package handlers

import (
	"cmp"
	"context"
	"fmt"
	"time"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
)

// ─── Actions Day-2 ──────────────────────────────────────────────────────────

type actionParams struct {
	Service    string            `json:"service"`
	Action     string            `json:"action"`
	Inputs     map[string]string `json:"inputs"`
	WebhookURL string            `json:"webhook_url,omitempty"`
	CIProvider string            `json:"ci_provider,omitempty"`
	Repo       string            `json:"repo,omitempty"`
	Ref        string            `json:"ref,omitempty"` // branche du repo ; vide : main
	Vars       map[string]string `json:"vars,omitempty"`
}

func (d TaskDeps) callActionWebhook(_ context.Context, x *tasks.Exec) error {
	var p actionParams
	if err := x.Params(&p); err != nil {
		return err
	}
	status, err := callWebhook(p.WebhookURL, map[string]any{
		"service":      p.Service,
		"action":       p.Action,
		"inputs":       p.Inputs,
		"task_id":      x.Task().ID,
		"triggered_at": time.Now().UTC().Format(time.RFC3339),
	})
	x.Set("http_status", status)
	if err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	return nil
}

// runActionPipeline lance la pipeline de l'action, puis attend sa fin
func (d TaskDeps) runActionPipeline(_ context.Context, x *tasks.Exec) error {
	var p actionParams
	if err := x.Params(&p); err != nil {
		return err
	}
	ci, err := d.Providers.CI(p.CIProvider)
	if err != nil {
		return err
	}
	var pipeline provider.Pipeline
	if !x.Get("pipeline", &pipeline) {
		x.Progress("lancement sur %s", p.Repo)
		started, err := ci.TriggerPipeline(p.Repo, cmp.Or(p.Ref, "main"), p.Vars)
		if err != nil {
			return fmt.Errorf("pipeline: %w", err)
		}
		pipeline = *started
		x.Set("pipeline", pipeline)
		d.DB.CreatePipeline(&database.Pipeline{
			ProjectName: p.Repo,
			PipelineID:  pipeline.ID,
			CIProvider:  ci.ID(),
			Type:        "action",
			Status:      pipeline.Status,
			WebURL:      pipeline.WebURL,
			Ref:         pipeline.Ref,
			TriggeredBy: x.Task().CreatedBy,
		})
	}
	x.WaitPipeline(ci.ID(), p.Repo, pipeline.ID, pipeline.WebURL, time.Now())
	return nil
}
