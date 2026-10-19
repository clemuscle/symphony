package main

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/notify"
	"github.com/clemuscle/symphony/internal/provider"
)

// buildNotifications : la section notifications, vérifiée contre les
// providers notifications de la configuration. Sans section : personne
// n'est prévenu hors de Symphony.
func buildNotifications(cfg *config.Config, providers *provider.Registry) (*notify.Settings, error) {
	nc := cfg.Notifications
	if nc == nil {
		return nil, nil
	}
	var errs []error
	// Schéma seulement : symphony validate laisse les ${VAR} non définies telles quelles
	if nc.URL != "" && !strings.HasPrefix(nc.URL, "https://") && !strings.HasPrefix(nc.URL, "http://") {
		errs = append(errs, fmt.Errorf("notifications.url %q : URL http(s) attendue (Symphony vu par les utilisateurs)", nc.URL))
	}
	check := func(where string, channels []string) {
		if len(channels) == 0 {
			errs = append(errs, fmt.Errorf("%s : au moins un canal", where))
		}
		for _, id := range channels {
			if _, err := providers.Notifications(id); err != nil {
				errs = append(errs, fmt.Errorf("%s : canal %q absent de providers.notifications", where, id))
			}
		}
	}
	teams := make([]string, 0, len(nc.Teams))
	for team := range nc.Teams {
		teams = append(teams, team)
	}
	sort.Strings(teams)
	for _, team := range teams {
		check("notifications.teams."+team, nc.Teams[team])
	}
	if nc.Default != nil {
		check("notifications.default", nc.Default)
	}
	if len(nc.Teams) == 0 && len(nc.Default) == 0 {
		errs = append(errs, errors.New("notifications : teams ou default requis"))
	}
	if err := errors.Join(errs...); err != nil {
		return nil, err
	}
	return &notify.Settings{URL: nc.URL, Teams: nc.Teams, Default: nc.Default}, nil
}
