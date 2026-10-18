package gitops

import (
	"errors"
	"testing"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/provider"
)

// reposSCM sert une fiche par repo ; broken simule un repo illisible
type reposSCM struct {
	provider.SCMProvider
	fiches map[string]string
	broken map[string]bool
}

func (f *reposSCM) ID() string       { return "fake" }
func (f *reposSCM) Platform() string { return "gitlab" }

func (f *reposSCM) GetFileContent(repo, branch, filePath string) (string, error) {
	if f.broken[repo] {
		return "", errors.New("gitlab injoignable")
	}
	content, ok := f.fiches[repo+"/"+filePath]
	if !ok {
		return "", provider.ErrNotFound
	}
	return content, nil
}

func TestSyncerReadsProjectFiches(t *testing.T) {
	scm := &reposSCM{
		fiches: map[string]string{
			"apps/auth/catalog-info.yaml":    "kind: Service\nmetadata:\n  name: autre-nom\n  owner: team-platform\n  tier: critical\n",
			"apps/invalid/catalog-info.yaml": "kind: GoldenPath\n",
		},
		broken: map[string]bool{},
	}
	r := provider.NewRegistry()
	r.RegisterSCM(scm, true)
	store := catalogue.NewStore()
	projects := []ProjectRef{
		{Name: "auth", RepoPath: "apps/auth", Branch: "main"},
		{Name: "sans-fiche", RepoPath: "apps/sans-fiche", Branch: "main"},
		{Name: "invalid", RepoPath: "apps/invalid", Branch: "main"},
	}
	s := NewSyncer(r, store, func() ([]ProjectRef, error) { return projects, nil }, "")

	s.ForceSync()
	svc, ok := store.Get("auth")
	if !ok {
		t.Fatalf("fiche de auth absente : %v", store.All())
	}
	// Le nom vient du projet, pas de la fiche
	if svc.Metadata.Name != "auth" || svc.Metadata.Owner != "team-platform" || svc.Metadata.Tier != "critical" {
		t.Errorf("fiche lue : %+v", svc.Metadata)
	}
	if store.Count() != 1 {
		t.Errorf("seule la fiche valide est au catalogue : %d", store.Count())
	}
	// Fiches invalides, absentes et à l'ancien format rapportées (page
	// Administration)
	report := s.Report()
	if len(report.Errors) != 1 || report.Errors[0].Project != "invalid" ||
		len(report.Missing) != 1 || report.Missing[0] != "sans-fiche" || report.File != "catalog-info.yaml" ||
		len(report.Legacy) != 1 || report.Legacy[0] != "auth" {
		t.Errorf("rapport = %+v", report)
	}

	// Repo illisible : la fiche connue est gardée
	scm.broken["apps/auth"] = true
	s.ForceSync()
	if _, ok := store.Get("auth"); !ok {
		t.Error("fiche perdue sur une erreur passagère")
	}

	// Projet supprimé : sa fiche disparaît
	scm.broken["apps/auth"] = false
	projects = projects[1:]
	s.ForceSync()
	if _, ok := store.Get("auth"); ok {
		t.Error("fiche d'un projet supprimé encore au catalogue")
	}
}
