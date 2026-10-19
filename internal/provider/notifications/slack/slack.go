// Package slack : canal de notification Slack, par webhook entrant
// (https://api.slack.com/messaging/webhooks)
package slack

import (
	"strings"

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
func (p *Provider) Type() string { return "slack" }

// marks : pictogramme de début de message, selon le niveau
var marks = map[string]string{
	provider.NotifySuccess:  ":white_check_mark:",
	provider.NotifyFailure:  ":x:",
	provider.NotifyApproval: ":raised_hand:",
	provider.NotifyInfo:     ":information_source:",
}

// Notify publie un message mrkdwn : titre en gras, texte, lien
func (p *Provider) Notify(n provider.Notification) error {
	var b strings.Builder
	if m := marks[n.Level]; m != "" {
		b.WriteString(m + " ")
	}
	b.WriteString("*" + escape(n.Title) + "*")
	if n.Text != "" {
		b.WriteString("\n" + escape(n.Text))
	}
	if n.URL != "" {
		b.WriteString("\n<" + n.URL + "|Ouvrir dans Symphony>")
	}
	return p.webhook.Post(map[string]string{"text": b.String()})
}

// escape : les trois caractères que Slack interprète (liens, mentions)
func escape(s string) string {
	return strings.NewReplacer("&", "&amp;", "<", "&lt;", ">", "&gt;").Replace(s)
}

// Check : un webhook ne se vérifie qu'en publiant ; rien n'est envoyé
func (p *Provider) Check() (string, error) {
	return "webhook " + p.webhook.Host() + " (non testé : un essai publierait un message)", nil
}
