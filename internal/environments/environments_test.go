package environments

import (
	"strings"
	"testing"
)

func TestValidate(t *testing.T) {
	known := func(rt string) bool { return rt == "docker-local" || rt == "argocd-k3s" }
	ok := []Environment{{Name: "staging", Runtime: "docker-local", Approval: "none"}, {Name: "production", Runtime: "argocd-k3s", Protected: true, Approval: ApprovalLead}}
	if err := Validate(ok, known); err != nil {
		t.Fatal(err)
	}
	bad := []Environment{
		{Name: "Staging", Runtime: "docker-local", Protected: true},
		{Name: "prod", Runtime: "kubernetes"},
		{Name: "prod", Runtime: "docker-local", Approval: "admin"},
	}
	err := Validate(bad, known)
	for _, want := range []string{"nom invalide", "ne peut pas être protégé", `runtime "kubernetes" inconnu`, "nom en double", `approval "admin" inconnu`} {
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("erreur = %v, attendu %q", err, want)
		}
	}
}

func TestPrevious(t *testing.T) {
	h := NewHolder([]Environment{{Name: "dev"}, {Name: "staging"}, {Name: "production", Protected: true}})
	if p, ok := h.Previous("production"); !ok || p.Name != "staging" {
		t.Errorf("précédent de production : %v %v", p, ok)
	}
	if _, ok := h.Previous("dev"); ok {
		t.Error("dev n'a pas de précédent")
	}
	if _, _, ok := h.Find("absent"); ok {
		t.Error("environnement absent trouvé")
	}
	if len(NewHolder(nil).List()) != 0 {
		t.Error("liste vide attendue")
	}
}
