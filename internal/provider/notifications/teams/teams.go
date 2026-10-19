// Package teams : canal de notification Microsoft Teams, par webhook d'un
// workflow (Power Automate, « Post to a channel when a webhook request is
// received ») qui reçoit une carte adaptative
package teams

import (
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/notifications"
)

type Provider struct {
	id      string
	webhook *notifications.Webhook
}

func New(id, webhookURL string) (*Provider, error) {
	w, err := notifications.NewWebhook(webhookURL)
	if err != nil {
		return nil, err
	}
	return &Provider{id: id, webhook: w}, nil
}

func (p *Provider) ID() string   { return p.id }
func (p *Provider) Type() string { return "teams" }

// colors : couleur du titre selon le niveau (noms de la carte adaptative)
var colors = map[string]string{
	provider.NotifySuccess:  "Good",
	provider.NotifyFailure:  "Attention",
	provider.NotifyApproval: "Warning",
}

// Notify publie une carte adaptative : titre, texte, bouton vers Symphony
func (p *Provider) Notify(n provider.Notification) error {
	title := map[string]any{"type": "TextBlock", "text": n.Title, "weight": "Bolder", "size": "Medium", "wrap": true}
	if c := colors[n.Level]; c != "" {
		title["color"] = c
	}
	body := []any{title}
	if n.Text != "" {
		body = append(body, map[string]any{"type": "TextBlock", "text": n.Text, "wrap": true})
	}
	card := map[string]any{
		"$schema": "http://adaptivecards.io/schemas/adaptive-card.json",
		"type":    "AdaptiveCard",
		"version": "1.4",
		"body":    body,
	}
	if n.URL != "" {
		card["actions"] = []any{map[string]any{"type": "Action.OpenUrl", "title": "Ouvrir dans Symphony", "url": n.URL}}
	}
	return p.webhook.Post(map[string]any{
		"type": "message",
		"attachments": []any{map[string]any{
			"contentType": "application/vnd.microsoft.card.adaptive",
			"content":     card,
		}},
	})
}

// Check : un webhook ne se vérifie qu'en publiant ; rien n'est envoyé
func (p *Provider) Check() (string, error) {
	return "webhook " + p.webhook.Host() + " (non testé : un essai publierait un message)", nil
}
