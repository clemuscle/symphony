package main

import (
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
)

// buildDiscovery : la section catalogue.discovery, vérifiée contre les
// providers de la configuration. Sans section : pas de découverte.
func buildDiscovery(cfg *config.Config, providers *provider.Registry) (gitops.DiscoverySettings, error) {
	dc := cfg.Catalogue.Discovery
	if dc == nil {
		return gitops.DiscoverySettings{}, nil
	}
	settings := gitops.DiscoverySettings{Interval: gitops.DefaultDiscoveryInterval}
	if dc.Interval != "" {
		d, err := time.ParseDuration(dc.Interval)
		if err != nil {
			return settings, fmt.Errorf("catalogue.discovery.interval %q : durée invalide (ex. 5m, 1h)", dc.Interval)
		}
		if d < time.Minute {
			return settings, fmt.Errorf("catalogue.discovery.interval %s : 1m minimum", dc.Interval)
		}
		settings.Interval = d
	}
	if len(dc.Sources) == 0 {
		return settings, errors.New("catalogue.discovery.sources : au moins un groupe à scanner")
	}
	var errs []error
	seen := map[string]bool{}
	for i, s := range dc.Sources {
		where := fmt.Sprintf("catalogue.discovery.sources[%d]", i)
		fail := func(format string, a ...any) {
			errs = append(errs, fmt.Errorf("%s : %s", where, fmt.Sprintf(format, a...)))
		}
		group := strings.Trim(s.Group, "/")
		if group == "" {
			fail("group requis (groupe GitLab ou organisation GitHub)")
			continue
		}
		scm, err := providers.SCM(s.SCM)
		if err != nil {
			fail("scm : %v", err)
			continue
		}
		ci, err := providers.CI(s.CI)
		if err != nil {
			fail("ci : %v", err)
			continue
		}
		if err := provider.CheckCompatible(scm, ci); err != nil {
			fail("%v", err)
			continue
		}
		src := gitops.DiscoverySource{SCM: scm.ID(), Group: group, CI: ci.ID(), Exclude: s.Exclude}
		// Registry facultatif, comme pour une stack : sans lui, l'image se
		// saisit au déploiement
		if reg, err := providers.GetRegistry(s.Registry); err == nil {
			src.Registry = reg.ID()
		} else if s.Registry != "" {
			fail("registry : %v", err)
			continue
		}
		for _, pattern := range s.Exclude {
			if _, err := path.Match(pattern, ""); err != nil {
				fail("exclude %q : motif invalide", pattern)
			}
		}
		if seen[src.Key()] {
			fail("groupe %s déjà scanné", src.Key())
			continue
		}
		seen[src.Key()] = true
		settings.Sources = append(settings.Sources, src)
	}
	return settings, errors.Join(errs...)
}

// directoryTeams : les équipes de l'annuaire (provider identity par défaut),
// sans les groupes qui donnent un rôle ; nil sans annuaire ou s'il ne répond
// pas (le scan ne retire alors aucun droit)
func directoryTeams(providers *provider.Registry, security *auth.Holder) func() map[string]bool {
	return func() map[string]bool {
		idp, err := providers.Identity("")
		if err != nil {
			return nil
		}
		groups, err := idp.ListGroups()
		if err != nil {
			if !errors.Is(err, provider.ErrUnsupported) {
				slog.Warn("découverte : annuaire illisible, équipes des fiches non vérifiées", "error", err)
			}
			return nil
		}
		teams := map[string]bool{}
		for _, g := range groups {
			if !security.IsRoleGroup(g.Name) {
				teams[g.Name] = true
			}
		}
		return teams
	}
}
