// Package notifications : ce que partagent les canaux de notification
// (Slack, Microsoft Teams) : un webhook entrant qui reçoit du JSON.
package notifications

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

// Webhook : l'URL d'un webhook entrant, qui vaut un secret (quiconque la
// connaît publie dans le canal) : jamais journalisée en entier
type Webhook struct {
	url    string
	client *http.Client
}

// NewWebhook vérifie l'URL : http(s), avec un hôte
func NewWebhook(raw string) (*Webhook, error) {
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http") || u.Host == "" {
		return nil, fmt.Errorf("webhook_url : URL http(s) attendue")
	}
	return &Webhook{url: raw, client: &http.Client{Timeout: 10 * time.Second}}, nil
}

// Host : l'hôte du webhook, seule partie montrée (page Administration, erreurs)
func (w *Webhook) Host() string {
	u, _ := url.Parse(w.url)
	return u.Host
}

// Post envoie payload en JSON ; une réponse hors 2xx est une erreur
func (w *Webhook) Post(payload any) error {
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	resp, err := w.client.Post(w.url, "application/json", bytes.NewReader(body))
	if err != nil {
		// L'erreur de net/http contient l'URL complète : seulement l'hôte
		return fmt.Errorf("webhook %s injoignable", w.Host())
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<10))
	return provider.CheckStatus("webhook "+w.Host(), resp.StatusCode, msg)
}
