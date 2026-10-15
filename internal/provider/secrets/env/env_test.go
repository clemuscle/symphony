package env

import (
	"errors"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
)

func TestGet(t *testing.T) {
	t.Setenv("SYMPHONY_TEST_SECRET", "valeur")
	p := New("env")
	if v, err := p.Get("SYMPHONY_TEST_SECRET", ""); err != nil || v != "valeur" {
		t.Errorf("Get = %q, %v", v, err)
	}
	if _, err := p.Get("SYMPHONY_TEST_ABSENT", ""); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("variable absente : %v", err)
	}
	if _, err := p.Get("SYMPHONY_TEST_SECRET", "cle"); err == nil {
		t.Error("clé acceptée pour une variable d'environnement")
	}
}
