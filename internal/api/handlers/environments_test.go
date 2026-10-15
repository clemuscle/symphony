package handlers

import (
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/catalogue"
)

// TestAppEnv : variables d'un environnement lues dans la fiche ; un projet ne
// lit que ses secrets (apps/<projet>/), jamais l'environnement de Symphony
func TestAppEnv(t *testing.T) {
	store := catalogue.NewStore()
	svc := catalogue.Service{}
	svc.Metadata.Name = "payment-api"
	svc.Spec.Environments = []catalogue.Environment{
		{Name: "staging", Env: map[string]string{
			"LOG_LEVEL":   "debug",
			"DB_PASSWORD": "secret://vault/apps/payment-api/db#password",
		}},
		{Name: "production", Env: map[string]string{"LOG_LEVEL": "info"}},
	}
	store.Replace([]catalogue.Service{svc})

	plain, secrets, err := appEnv(store, "payment-api", "staging")
	if err != nil {
		t.Fatal(err)
	}
	if plain["LOG_LEVEL"] != "debug" || len(plain) != 1 {
		t.Errorf("en clair : %v", plain)
	}
	if ref := secrets["DB_PASSWORD"]; ref.Provider != "vault" || ref.Path != "apps/payment-api/db" || ref.Key != "password" {
		t.Errorf("secrets : %v", secrets)
	}
	if plain, _, _ := appEnv(store, "payment-api", "production"); plain["LOG_LEVEL"] != "info" {
		t.Errorf("production : %v", plain)
	}
	if plain, secrets, err := appEnv(store, "inconnu", "staging"); err != nil || len(plain)+len(secrets) != 0 {
		t.Errorf("projet sans fiche : %v %v %v", plain, secrets, err)
	}

	for value, want := range map[string]string{
		"secret://vault/symphony/gitlab#token":     "ne lit que ses secrets, sous apps/payment-api/",
		"secret://vault/apps/auth-service/db#pass": "ne lit que ses secrets",
		"secret://env/GITLAB_TOKEN":                "variables d'environnement de Symphony",
		"secret://vault/apps/payment-api":          "",
	} {
		svc.Spec.Environments = []catalogue.Environment{{Name: "staging", Env: map[string]string{"X": value}}}
		store.Replace([]catalogue.Service{svc})
		_, _, err := appEnv(store, "payment-api", "staging")
		if want == "" {
			if err == nil {
				t.Errorf("%s : chemin hors du dossier du projet accepté", value)
			}
			continue
		}
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s : erreur = %v, attendu %q", value, err, want)
		}
	}
}
