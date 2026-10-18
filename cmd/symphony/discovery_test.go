package main

import (
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/config"
)

const discoveryProviders = `
providers:
  scm:
    - { id: gitlab, type: gitlab, default: true, config: { url: http://gitlab, token: x } }
    - { id: github, type: github, config: { token: x } }
  ci:
    - { id: gitlab-ci, type: gitlab_ci, default: true, config: { url: http://gitlab, scm_ref: gitlab } }
    - { id: github-actions, type: github_actions, config: { scm_ref: github } }
`

func TestBuildDiscovery(t *testing.T) {
	build := func(catalogue string) (int, time.Duration, error) {
		t.Helper()
		cfg, err := config.Parse([]byte(discoveryProviders + catalogue))
		if err != nil {
			t.Fatal(err)
		}
		providers, resolved, err := buildProviders(cfg)
		if err != nil {
			t.Fatal(err)
		}
		s, err := buildDiscovery(resolved, providers)
		return len(s.Sources), s.Interval, err
	}

	if n, _, err := build(""); n != 0 || err != nil {
		t.Errorf("sans section : %d sources, %v", n, err)
	}
	n, interval, err := build(`catalogue:
  discovery:
    interval: 10m
    sources:
      - { group: apps/legacy, exclude: ["apps/legacy/old-*"] }
      - { group: acme, scm: github, ci: github-actions }
`)
	if n != 2 || interval != 10*time.Minute || err != nil {
		t.Errorf("sources : %d, %s, %v", n, interval, err)
	}

	for catalogue, want := range map[string]string{
		"catalogue:\n  discovery: { sources: [] }\n":                              "au moins un groupe",
		"catalogue:\n  discovery: { interval: 10s, sources: [{group: a}] }\n":     "1m minimum",
		"catalogue:\n  discovery: { interval: bientôt, sources: [{group: a}] }\n": "durée invalide",
		"catalogue:\n  discovery: { sources: [{scm: gitlab}] }\n":                 "group requis",
		"catalogue:\n  discovery: { sources: [{group: acme, scm: github}] }\n":    "incompatible", // CI GitLab par défaut
		"catalogue:\n  discovery: { sources: [{group: a, scm: inconnu}] }\n":      "scm",
		"catalogue:\n  discovery: { sources: [{group: a, registry: inconnu}] }\n": "registry",
		"catalogue:\n  discovery: { sources: [{group: a, exclude: ['[']}] }\n":    "motif invalide",
		"catalogue:\n  discovery: { sources: [{group: a}, {group: /a/}] }\n":      "déjà scanné",
		"catalogue:\n  discovery: { sources: [{group: a, branche: main}] }\n":     "branche", // clé inconnue
	} {
		cfg, err := config.Parse([]byte(discoveryProviders + catalogue))
		if err == nil {
			providers, resolved, berr := buildProviders(cfg)
			if berr != nil {
				t.Fatal(berr)
			}
			_, err = buildDiscovery(resolved, providers)
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s : %v, %q attendu", catalogue, err, want)
		}
	}
}
