package notifications_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/notifications/slack"
	"github.com/clemuscle/symphony/internal/provider/notifications/teams"
)

var msg = provider.Notification{
	Title: "Approbation demandée : api en production",
	Text:  "bob demande <img:2>",
	URL:   "https://symphony.test/tasks",
	Level: provider.NotifyApproval,
}

// capture : un webhook qui garde le dernier corps reçu
func capture(t *testing.T, status int) (*httptest.Server, *map[string]any) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("Content-Type %q", r.Header.Get("Content-Type"))
		}
		json.NewDecoder(r.Body).Decode(&got)
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv, &got
}

func TestSlack(t *testing.T) {
	srv, got := capture(t, 200)
	p, err := slack.New("slack", srv.URL+"/services/T/B/secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Notify(msg); err != nil {
		t.Fatal(err)
	}
	text, _ := (*got)["text"].(string)
	for _, want := range []string{":raised_hand: *Approbation demandée", "bob demande &lt;img:2&gt;", "<https://symphony.test/tasks|Ouvrir dans Symphony>"} {
		if !strings.Contains(text, want) {
			t.Errorf("texte %q sans %q", text, want)
		}
	}
	if detail, err := p.Check(); err != nil || strings.Contains(detail, "secret") {
		t.Errorf("Check : %q, %v", detail, err)
	}
}

func TestTeams(t *testing.T) {
	srv, got := capture(t, 202)
	p, err := teams.New("teams", srv.URL+"/workflows/secret")
	if err != nil {
		t.Fatal(err)
	}
	if err := p.Notify(msg); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(*got)
	for _, want := range []string{`"type":"message"`, `"contentType":"application/vnd.microsoft.card.adaptive"`,
		`"type":"AdaptiveCard"`, `"color":"Warning"`, `"url":"https://symphony.test/tasks"`} {
		if !strings.Contains(string(raw), want) {
			t.Errorf("carte %s sans %s", raw, want)
		}
	}
}

// Une URL de webhook vaut un secret : ni les erreurs ni Check ne la montrent
func TestWebhookSecret(t *testing.T) {
	srv, _ := capture(t, 404)
	p, _ := slack.New("slack", srv.URL+"/services/T/B/secret")
	err := p.Notify(msg)
	if err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("erreur : %v", err)
	}
	p, _ = slack.New("slack", "http://127.0.0.1:1/services/T/B/secret")
	if err := p.Notify(msg); err == nil || strings.Contains(err.Error(), "secret") {
		t.Errorf("webhook injoignable : %v", err)
	}
	for _, bad := range []string{"", "hooks.slack.com/x", "ftp://x/y", "https://"} {
		if _, err := teams.New("teams", bad); err == nil {
			t.Errorf("URL %q acceptée", bad)
		}
	}
}
