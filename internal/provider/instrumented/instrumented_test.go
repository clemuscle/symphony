package instrumented

import (
	"errors"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
)

type fakeSCM struct{ provider.SCMProvider }

func (fakeSCM) ID() string       { return "fake" }
func (fakeSCM) Platform() string { return "gitlab" }
func (fakeSCM) DeleteFile(_, _, _, _ string) error {
	return provider.ErrNotFound
}
func (fakeSCM) GetFileContent(_, _, path string) (string, error) { return "contenu de " + path, nil }

// L'enveloppe transmet résultats et erreurs inchangés
func TestSCM(t *testing.T) {
	p := SCM(fakeSCM{})
	if p.ID() != "fake" || p.Platform() != "gitlab" {
		t.Errorf("ID/Platform = %s/%s", p.ID(), p.Platform())
	}
	if got, err := p.GetFileContent("r", "main", "a.yaml"); err != nil || got != "contenu de a.yaml" {
		t.Errorf("GetFileContent = %q, %v", got, err)
	}
	if err := p.DeleteFile("r", "main", "a.yaml", "msg"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("DeleteFile = %v, attendu ErrNotFound", err)
	}
}
