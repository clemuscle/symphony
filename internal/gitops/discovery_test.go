package gitops

import (
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/fake"
)

// memStore : les projets en mémoire
type memStore struct{ projects map[string]FoundProject }

func (m *memStore) KnownProjects() ([]KnownProject, error) {
	var out []KnownProject
	for _, p := range m.projects {
		out = append(out, KnownProject{Name: p.Name, RepoPath: p.Repo.Path, SCMProvider: p.Source.SCM, DiscoveredFrom: p.Source.Key()})
	}
	return out, nil
}
func (m *memStore) AddProject(p FoundProject) error    { m.projects[p.Name] = p; return nil }
func (m *memStore) UpdateProject(p FoundProject) error { m.projects[p.Name] = p; return nil }

func component(name, owner string) string {
	return "apiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n  name: " + name +
		"\n  annotations: {symphony.io/port: \"9000\"}\nspec:\n  type: service\n  lifecycle: production\n  owner: " + owner + "\n"
}

func TestDiscoveryScan(t *testing.T) {
	forge := fake.NewForge(nil)
	forge.Seed("legacy/billing", map[string]string{"catalog-info.yaml": component("billing", "group:default/team-payments")})
	forge.Seed("legacy/sans-fiche", map[string]string{"README.md": "x"})
	forge.Seed("legacy/casse", map[string]string{"catalog-info.yaml": "kind: ["})
	forge.Seed("legacy/old-thing", map[string]string{"catalog-info.yaml": component("old-thing", "team-payments")})
	forge.Seed("legacy/doublon", map[string]string{"catalog-info.yaml": component("auth", "team-identity")})
	forge.Seed("ailleurs/autre", map[string]string{"catalog-info.yaml": component("autre", "team-x")})
	r := provider.NewRegistry()
	r.RegisterSCM(forge.SCM(), true)

	// auth : créé par Symphony ailleurs (source sans groupe, voir createdStore)
	store := &memStore{projects: map[string]FoundProject{
		"auth": {Name: "auth", Repo: provider.Repo{Path: "apps/auth"}, Source: DiscoverySource{SCM: "gitlab"}},
	}}
	changed := 0
	d := NewDiscoverer(r, &createdStore{memStore: store}, func() string { return "catalog-info.yaml" }, func() { changed++ })
	// L'annuaire ne connaît pas team-identity : le conflit de nom suffit
	d.Teams = func() map[string]bool { return map[string]bool{"team-payments": true, "team-finance": true} }
	src := DiscoverySource{SCM: "gitlab", Group: "legacy", CI: "gitlab-ci", Exclude: []string{"legacy/old-*"}}
	d.SetSettings(DiscoverySettings{Interval: time.Minute, Sources: []DiscoverySource{src}})
	d.Scan()

	billing, ok := store.projects["billing"]
	if !ok || billing.Team != "team-payments" || billing.Port != 9000 || billing.Repo.Path != "legacy/billing" ||
		billing.Source.Key() != "gitlab/legacy" {
		t.Fatalf("billing : %+v (projets %v)", billing, store.projects)
	}
	if _, ok := store.projects["old-thing"]; ok {
		t.Error("repo exclu importé")
	}
	if changed != 1 || !d.Matched("billing") {
		t.Errorf("onChange=%d, matched=%v", changed, d.Matched("billing"))
	}
	rep := d.Report()
	sr := rep.Sources[0]
	if !rep.Enabled || sr.Excluded != 1 || sr.NoFiche != 1 || sr.Repos != 4 || len(sr.Imported) != 1 {
		t.Errorf("rapport de la source : %+v", sr)
	}
	// Fiche cassée et nom déjà pris
	errs := map[string]string{}
	for _, e := range rep.Errors {
		errs[e.Repo] = e.Error
	}
	if len(errs) != 2 || errs["legacy/casse"] == "" || !strings.Contains(errs["legacy/doublon"], "déjà pris") {
		t.Errorf("erreurs : %+v", rep.Errors)
	}

	// Deuxième scan : rien de nouveau, l'équipe suit la fiche
	forge.Seed("legacy/billing", map[string]string{"catalog-info.yaml": component("billing", "team-finance")})
	d.Scan()
	if changed != 1 || store.projects["billing"].Team != "team-finance" {
		t.Errorf("mise à jour : onChange=%d, équipe %q", changed, store.projects["billing"].Team)
	}

	// Fiche retirée (le repo est exclu) : orphelin, toujours là
	src.Exclude = append(src.Exclude, "legacy/billing")
	d.SetSettings(DiscoverySettings{Interval: time.Minute, Sources: []DiscoverySource{src}})
	d.Scan()
	rep = d.Report()
	if len(rep.Orphans) != 1 || rep.Orphans[0].Project != "billing" || d.Matched("billing") {
		t.Errorf("orphelins : %+v", rep.Orphans)
	}
	if _, ok := store.projects["billing"]; !ok {
		t.Error("projet orphelin supprimé")
	}
}

// Une source illisible ne rend pas ses projets orphelins
func TestDiscoveryUnreadableSource(t *testing.T) {
	forge := fake.NewForge(nil)
	r := provider.NewRegistry()
	r.RegisterSCM(forge.SCM(), true)
	src := DiscoverySource{SCM: "gitlab", Group: "legacy"}
	store := &memStore{projects: map[string]FoundProject{
		"billing": {Name: "billing", Repo: provider.Repo{Path: "legacy/billing"}, Source: src},
	}}
	d := NewDiscoverer(r, store, func() string { return "catalog-info.yaml" }, nil)
	d.SetSettings(DiscoverySettings{Sources: []DiscoverySource{src}})
	d.Scan() // groupe absent du fake : ErrNotFound
	rep := d.Report()
	if rep.Sources[0].Error == "" || len(rep.Orphans) != 0 || !d.Matched("billing") {
		t.Errorf("rapport : %+v", rep)
	}

	// Sans source : rien
	d.SetSettings(DiscoverySettings{})
	d.Scan()
	if rep := d.Report(); rep.Enabled || len(rep.Sources) != 0 {
		t.Errorf("découverte désactivée : %+v", rep)
	}
}

// createdStore : les projets dont la source n'a pas de groupe sont ceux
// créés par Symphony
type createdStore struct{ *memStore }

func (c *createdStore) KnownProjects() ([]KnownProject, error) {
	known, _ := c.memStore.KnownProjects()
	for i := range known {
		if strings.HasSuffix(known[i].DiscoveredFrom, "/") {
			known[i].DiscoveredFrom = ""
		}
	}
	return known, nil
}
