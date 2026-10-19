// Package notify prévient les équipes de ce qui arrive à leurs projets
// (approbation demandée, déploiement réussi ou échoué) dans leurs canaux :
// providers notifications (Slack, Teams), choisis par équipe dans la section
// notifications de la configuration. Les envois partent en arrière-plan : un
// canal injoignable ne retarde ni ne fait échouer une tâche.
package notify

import (
	"log/slog"
	"strings"
	"sync/atomic"

	"github.com/clemuscle/symphony/internal/provider"
)

// Settings : la section notifications, vérifiée et résolue
type Settings struct {
	// URL : Symphony vu par les utilisateurs, pour les liens des messages
	URL string
	// Teams : équipe → ids des canaux (providers notifications)
	Teams map[string][]string
	// Default : canaux des projets dont l'équipe n'a pas les siens
	Default []string
}

// Dispatcher envoie les notifications aux canaux de chaque équipe. Les
// réglages sont remplacés au rechargement de la configuration ; les providers
// sont résolus à chaque envoi.
type Dispatcher struct {
	providers *provider.Registry
	settings  atomic.Pointer[Settings]
	// sent : appelé après chaque envoi (tests), err nil si réussi
	sent func(channel string, err error)
}

func New(providers *provider.Registry, s *Settings) *Dispatcher {
	d := &Dispatcher{providers: providers}
	d.Set(s)
	return d
}

func (d *Dispatcher) Set(s *Settings) {
	if s == nil {
		s = &Settings{}
	}
	d.settings.Store(s)
}

// Channels : les canaux de l'équipe team (ceux par défaut si elle n'en a pas)
func (d *Dispatcher) Channels(team string) []string {
	s := d.settings.Load()
	if ch, ok := s.Teams[team]; ok && team != "" {
		return ch
	}
	return s.Default
}

// Link : l'adresse de path dans Symphony, vide sans url configurée
func (d *Dispatcher) Link(path string) string {
	base := d.settings.Load().URL
	if base == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + path
}

// Send publie n dans les canaux de l'équipe team, en arrière-plan
func (d *Dispatcher) Send(team string, n provider.Notification) {
	if d == nil {
		return
	}
	for _, id := range d.Channels(team) {
		p, err := d.providers.Notifications(id)
		if err != nil {
			slog.Warn("notification", "canal", id, "error", err)
			continue
		}
		go func() {
			err := p.Notify(n)
			if err != nil {
				slog.Warn("notification non envoyée", "canal", id, "team", team, "error", err)
			}
			if d.sent != nil {
				d.sent(id, err)
			}
		}()
	}
}
