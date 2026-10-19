package main

import (
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/notifications/slack"
)

func TestBuildNotifications(t *testing.T) {
	providers := provider.NewRegistry()
	ch, _ := slack.New("slack-payments", "https://hooks.slack.test/services/x")
	providers.RegisterNotifications(ch)

	if s, err := buildNotifications(&config.Config{}, providers); err != nil || s != nil {
		t.Errorf("sans section : %+v, %v", s, err)
	}
	cfg := &config.Config{Notifications: &config.NotificationsConfig{
		URL:     "https://symphony.test",
		Teams:   map[string][]string{"team-payments": {"slack-payments"}},
		Default: []string{"slack-payments"},
	}}
	s, err := buildNotifications(cfg, providers)
	if err != nil || s.Teams["team-payments"][0] != "slack-payments" || s.URL != "https://symphony.test" {
		t.Errorf("notifications : %+v, %v", s, err)
	}

	cfg.Notifications = &config.NotificationsConfig{
		URL:     "symphony.test",
		Teams:   map[string][]string{"team-payments": {"slack-inconnu"}, "team-identity": {}},
		Default: []string{},
	}
	_, err = buildNotifications(cfg, providers)
	for _, want := range []string{"notifications.url", `canal "slack-inconnu" absent`, "notifications.teams.team-identity : au moins un canal",
		"notifications.default : au moins un canal"} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("erreur = %v, attendu %q", err, want)
		}
	}
	if _, err := buildNotifications(&config.Config{Notifications: &config.NotificationsConfig{}}, providers); err == nil {
		t.Error("section vide acceptée")
	}
}
