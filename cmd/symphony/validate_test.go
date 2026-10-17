package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func writeFiles(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, content := range files {
		p := filepath.Join(dir, name)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func validate(t *testing.T, args ...string) (int, string) {
	t.Helper()
	var out bytes.Buffer
	code := runValidate(args, &out)
	return code, out.String()
}

const validConfig = `
providers:
  secrets:
    - id: vault
      type: vault
      config:
        url: http://vault:8200
        role_id: secret://env/VAULT_ROLE_ID
        secret_id: secret://env/VAULT_SECRET_ID
  scm:
    - id: gitlab
      type: gitlab
      config:
        url: ${GITLAB_URL}
        token: secret://vault/symphony/gitlab#token
  ci:
    - id: gitlab-ci
      type: gitlab_ci
      config: { url: "${GITLAB_URL}", scm_ref: gitlab }
  runtime:
    - id: docker
      type: docker
environments:
  - { name: staging, runtime: docker }
  - { name: production, runtime: docker, protected: true }
webhooks:
  url: http://symphony:8080
  secret: secret://vault/symphony/webhook#secret
`

const template = `apiVersion: symphony/v1
kind: GoldenPath
metadata:
  name: api
spec:
  providers: { scm: gitlab, ci: gitlab-ci, runtime: docker }
  ci_template: ci.yml
`

// Une configuration complète passe sans rien lire : ni Vault, ni l'environnement
func TestValidateOK(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"symphony.yaml":               validConfig,
		"templates/api/template.yaml": template,
		"templates/api/ci.yml":        "stages: [build]",
		"templates/api/skeleton/main": "x",
	})
	code, out := validate(t, "-config", filepath.Join(dir, "symphony.yaml"), "-templates", filepath.Join(dir, "templates"))
	if code != 0 {
		t.Fatalf("code %d :\n%s", code, out)
	}
	for _, want := range []string{
		"variable GITLAB_URL non définie",
		"✓ scm gitlab (par défaut)",
		"✓ environnements : staging → production (protégé)",
		"pas de section auth",
		"✓ api : api",
		"✓ aucune erreur",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q absent :\n%s", want, out)
		}
	}
}

// Toutes les erreurs sont rapportées d'un coup, avec un code de sortie 1
func TestValidateErrors(t *testing.T) {
	bad := strings.NewReplacer(
		"type: docker", "type: docker\n      config: { sockett: /var/run/docker.sock }", // clé inconnue
		"secret://vault/symphony/gitlab#token", "secret://coffre/symphony/gitlab#token", // provider de secrets absent
	).Replace(validConfig)
	dir := writeFiles(t, map[string]string{
		"symphony.yaml":                       bad,
		"templates/api/template.yaml":         template,
		"templates/api/ci.yml":                "x",
		"templates/sans-ci/template.yaml":     strings.Replace(template, "name: api", "name: sans-ci", 1),
		"templates/sans-ci/skeleton/a":        "x",
		"templates/mauvais-nom/template.yaml": template,
	})
	code, out := validate(t, "-config", filepath.Join(dir, "symphony.yaml"), "-templates", filepath.Join(dir, "templates"))
	if code != 1 {
		t.Fatalf("code %d, attendu 1 :\n%s", code, out)
	}
	for _, want := range []string{
		"sockett",
		"configuration invalide : providers des golden paths non vérifiés",
		"skeleton api/skeleton introuvable", // pas de skeleton
		"différent du dossier",
		"ci_template",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q absent :\n%s", want, out)
		}
	}
}

// Un golden path qui désigne un provider absent de la configuration est refusé
func TestValidateTemplateProviders(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"symphony.yaml":               validConfig,
		"templates/api/template.yaml": strings.Replace(template, "runtime: docker", "runtime: argocd", 1),
		"templates/api/ci.yml":        "x",
		"templates/api/skeleton/a":    "x",
	})
	code, out := validate(t, "-config", filepath.Join(dir, "symphony.yaml"), "-templates", filepath.Join(dir, "templates"))
	if code != 1 || !strings.Contains(out, `runtime provider: runtime provider "argocd" not found`) {
		t.Errorf("code %d :\n%s", code, out)
	}
}

func TestValidateUsage(t *testing.T) {
	if code, out := validate(t); code != 2 || !strings.Contains(out, "symphony validate -config") {
		t.Errorf("code %d :\n%s", code, out)
	}
}

// Les configurations et golden paths livrés passent symphony validate
func TestValidateRepo(t *testing.T) {
	// Variables du quickstart (.env.example)
	for k, v := range map[string]string{
		"FORGE_URL": "https://gitlab.com", "REGISTRY_HOST": "registry.gitlab.com",
		"FORGE_GROUP": "mon-groupe", "TEMPLATES_REPO": "mon-groupe/symphony-templates",
	} {
		t.Setenv(k, v)
	}
	for _, args := range [][]string{
		{"-config", "../../quickstart/symphony.gitlab.yaml"},
		{"-config", "../../quickstart/symphony.github.yaml"},
		{"-config", "../../demo/gitlab/symphony-config/symphony.yaml", "-templates", "../../demo/gitlab/symphony-templates/templates"},
		{"-config", "../../demo/symphony.yaml"},
		{"-config", "../../symphony.yaml"},
		{"-config", "../../documentation/reference/symphony.yaml"},
		{"-config", "../../documentation/reference/bootstrap.yaml"},
	} {
		if code, out := validate(t, args...); code != 0 {
			t.Errorf("%v : code %d\n%s", args, code, out)
		}
	}
}

// Une référence vers un provider de secrets non déclaré est refusée sans
// rien lire
func TestValidateSecretRef(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"symphony.yaml": strings.Replace(validConfig, "secret://vault/symphony/gitlab#token", "secret://coffre/symphony/gitlab#token", 1),
	})
	code, out := validate(t, "-config", filepath.Join(dir, "symphony.yaml"))
	if code != 1 || !strings.Contains(out, "coffre") {
		t.Errorf("code %d :\n%s", code, out)
	}
}
