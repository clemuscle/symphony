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

// Le dernier appel de chaque provider est retenu ; une ressource introuvable
// n'est pas une panne du provider
func TestLast(t *testing.T) {
	p := SCM(fakeSCM{})
	p.DeleteFile("r", "main", "a.yaml", "msg")
	if c, ok := Last("scm", "fake"); !ok || c.Method != "DeleteFile" || c.Error != "" {
		t.Errorf("dernier appel = %+v, %v", c, ok)
	}
	p.Check()
	if c, _ := Last("scm", "fake"); c.Method != "Check" || c.Error != "jeton refusé" {
		t.Errorf("dernier appel = %+v", c)
	}
}

func (fakeSCM) Check() (string, error) { return "", errors.New("jeton refusé") }
