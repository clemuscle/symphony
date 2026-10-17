package main

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/config"
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
stacks:
  repo: platform/stacks
  official: [api-rest-go]
`

const stack = `apiVersion: symphony/v1
kind: Stack
metadata:
  name: api
spec:
  providers: { scm: gitlab, ci: gitlab-ci, runtime: docker }
  ci_template: ci.yml
  parameters:
    - { name: log_level, type: choice, choices: [info, debug], default: info }
`

// Une configuration complète passe sans rien lire : ni Vault, ni l'environnement
func TestValidateOK(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"symphony.yaml":            validConfig,
		"stacks/api/stack.yaml":    stack,
		"stacks/api/ci.yml":        "stages: [build]",
		"stacks/api/skeleton/main": "x",
		"stacks/old/template.yaml": strings.NewReplacer("kind: Stack", "kind: GoldenPath", "name: api", "name: old").Replace(stack),
		"stacks/old/ci.yml":        "x",
		"stacks/old/skeleton/main": "x",
	})
	code, out := validate(t, "-config", filepath.Join(dir, "symphony.yaml"), "-stacks", filepath.Join(dir, "stacks"))
	if code != 0 {
		t.Fatalf("code %d :\n%s", code, out)
	}
	for _, want := range []string{
		"variable GITLAB_URL non définie",
		"✓ scm gitlab (par défaut)",
		"✓ environnements : staging → production (protégé)",
		"pas de section auth",
		"✓ stacks lues dans platform/stacks (branche main, dossier stacks)",
		"✓ stack officielle api-rest-go",
		"✓ api : api (paramètres : log_level)",
		"ancien format",
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
		"symphony.yaml":                 bad,
		"stacks/api/stack.yaml":         stack,
		"stacks/api/ci.yml":             "x",
		"stacks/sans-ci/stack.yaml":     strings.Replace(stack, "name: api", "name: sans-ci", 1),
		"stacks/sans-ci/skeleton/a":     "x",
		"stacks/mauvais-nom/stack.yaml": stack,
		"stacks/param/stack.yaml":       strings.NewReplacer("name: api", "name: param", "default: info", "default: trace").Replace(stack),
	})
	code, out := validate(t, "-config", filepath.Join(dir, "symphony.yaml"), "-stacks", filepath.Join(dir, "stacks"))
	if code != 1 {
		t.Fatalf("code %d, attendu 1 :\n%s", code, out)
	}
	for _, want := range []string{
		"sockett",
		"configuration invalide : providers des stacks non vérifiés",
		"skeleton api/skeleton introuvable", // pas de skeleton
		"différent du dossier",
		"ci_template",
		"trace n'est pas l'un des choix",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("%q absent :\n%s", want, out)
		}
	}
}

// Une stack qui désigne un provider absent de la configuration est refusée
func TestValidateStackProviders(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"symphony.yaml":         validConfig,
		"stacks/api/stack.yaml": strings.Replace(stack, "runtime: docker", "runtime: argocd", 1),
		"stacks/api/ci.yml":     "x",
		"stacks/api/skeleton/a": "x",
	})
	code, out := validate(t, "-config", filepath.Join(dir, "symphony.yaml"), "-stacks", filepath.Join(dir, "stacks"))
	if code != 1 || !strings.Contains(out, `runtime provider: runtime provider "argocd" not found`) {
		t.Errorf("code %d :\n%s", code, out)
	}
}

func TestValidateUsage(t *testing.T) {
	if code, out := validate(t); code != 2 || !strings.Contains(out, "symphony validate -config") {
		t.Errorf("code %d :\n%s", code, out)
	}
}

// Une stack officielle inconnue fait refuser la configuration
func TestValidateUnknownOfficial(t *testing.T) {
	dir := writeFiles(t, map[string]string{
		"symphony.yaml": strings.Replace(validConfig, "official: [api-rest-go]", "official: [api-rest-cobol]", 1),
	})
	code, out := validate(t, "-config", filepath.Join(dir, "symphony.yaml"))
	if code != 1 || !strings.Contains(out, `stacks.official : "api-rest-cobol" inconnue`) {
		t.Errorf("code %d :\n%s", code, out)
	}
}

// Les configurations et stacks livrées passent symphony validate
func TestValidateRepo(t *testing.T) {
	// Variables du quickstart (.env.example)
	for k, v := range map[string]string{
		"FORGE_URL": "https://gitlab.com", "REGISTRY_HOST": "registry.gitlab.com",
		"FORGE_GROUP": "mon-groupe", "STACKS_REPO": "",
	} {
		t.Setenv(k, v)
	}
	for _, args := range [][]string{
		{"-config", "../../quickstart/symphony.gitlab.yaml"},
		{"-config", "../../quickstart/symphony.github.yaml"},
		{"-config", "../../demo/gitlab/symphony-config/symphony.yaml", "-stacks", "../../demo/gitlab/symphony-stacks/stacks"},
		{"-stacks", "../../stacks"},
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

// stacksSource : section stacks, ancienne section templates, ou rien
func TestStacksSource(t *testing.T) {
	parse := func(src string) *config.Config {
		cfg, err := config.Parse([]byte(src))
		if err != nil {
			t.Fatal(err)
		}
		return cfg
	}
	src, warnings, err := stacksSource(parse("stacks:\n  official: ['*']\n"))
	if err != nil || src.Repo != "" || src.Path != "stacks" || len(src.Official) != 1 || len(warnings) != 0 {
		t.Errorf("stacks : %+v %v %v", src, warnings, err)
	}
	src, warnings, err = stacksSource(parse("templates:\n  repo: root/t\n"))
	if err != nil || src.Repo != "root/t" || src.Path != "templates" || len(warnings) != 1 {
		t.Errorf("templates : %+v %v %v", src, warnings, err)
	}
	src, warnings, err = stacksSource(parse("catalogue: {}\n"))
	if err != nil || src.Repo != "root/symphony-templates" || len(warnings) != 1 {
		t.Errorf("sans section : %+v %v %v", src, warnings, err)
	}
	if _, _, err := stacksSource(parse("stacks: {repo: a}\ntemplates: {repo: b}\n")); err == nil {
		t.Error("stacks et templates : erreur attendue")
	}
	if _, warnings, _ := stacksSource(parse("stacks: {}\n")); len(warnings) != 1 {
		t.Errorf("aucune stack : %v", warnings)
	}
}
