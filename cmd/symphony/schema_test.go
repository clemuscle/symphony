package main

import (
	"os"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/environments"
)

func TestValidateProviders(t *testing.T) {
	gitlab := func(c map[string]string) config.ProviderDef {
		return config.ProviderDef{ID: "gitlab", Type: "gitlab", Config: c}
	}
	ok := map[string]string{"url": "http://gitlab", "token": "t"}
	cases := []struct {
		name string
		p    config.ProvidersConfig
		want string // vide : valide
	}{
		{"valide", config.ProvidersConfig{SCM: []config.ProviderDef{gitlab(ok)},
			CI: []config.ProviderDef{{ID: "ci", Type: "gitlab_ci", Config: map[string]string{"url": "u", "scm_ref": "gitlab"}}}}, ""},
		{"faute de frappe", config.ProvidersConfig{SCM: []config.ProviderDef{gitlab(map[string]string{"url": "u", "token": "t", "namspace": "apps"})}},
			"clé(s) inconnue(s) namspace"},
		{"ancien token_env", config.ProvidersConfig{SCM: []config.ProviderDef{gitlab(map[string]string{"url": "u", "token_env": "GITLAB_TOKEN"})}},
			"secret://env/<VARIABLE>"},
		{"clé requise absente", config.ProvidersConfig{SCM: []config.ProviderDef{gitlab(map[string]string{"url": "u"})}},
			"requise(s) absente(s) : token"},
		{"ni jeton ni scm_ref", config.ProvidersConfig{CI: []config.ProviderDef{{ID: "ci", Type: "gitlab_ci", Config: map[string]string{"url": "u"}}}},
			"l'une de ces clés est requise : token, scm_ref"},
		{"type inconnu", config.ProvidersConfig{Runtime: []config.ProviderDef{{ID: "k", Type: "kubernetes"}}},
			`type "kubernetes" inconnu (argocd, docker)`},
		{"id en double", config.ProvidersConfig{SCM: []config.ProviderDef{gitlab(ok), gitlab(ok)}}, "id en double"},
		{"id absent", config.ProvidersConfig{Runtime: []config.ProviderDef{{Type: "docker"}}}, "providers.runtime[0] : id requis"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			err := validateProviders(c.p)
			switch {
			case c.want == "" && err != nil:
				t.Fatalf("refusée : %v", err)
			case c.want != "" && (err == nil || !strings.Contains(err.Error(), c.want)):
				t.Fatalf("erreur = %v, attendu %q", err, c.want)
			}
		})
	}
}

// TestBuildProvidersStrict : un provider impossible à construire fait refuser
// la configuration, au lieu d'être ignoré
func TestBuildProvidersStrict(t *testing.T) {
	cfg, err := config.Parse([]byte(`
providers:
  scm:
    - id: gitlab
      type: gitlab
      config: {url: http://gitlab, token: t}
  ci:
    - id: ci
      type: gitlab_ci
      config: {url: http://gitlab, scm_ref: absent}
`))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := buildProviders(cfg); err == nil || !strings.Contains(err.Error(), `scm_ref "absent"`) {
		t.Errorf("erreur = %v", err)
	}
	// Clé inconnue à la racine du fichier
	if _, err := config.Parse([]byte("catalogue:\n  repo: root/c\n")); err == nil {
		t.Error("ancienne clé catalogue.repo acceptée")
	}
}

// TestRepoConfigsValid : les configurations livrées dans le repo (démo,
// bootstrap) respectent le schéma ; une démo cassée se voit dans la CI
func TestRepoConfigsValid(t *testing.T) {
	// Variables que la démo fournit toujours (docker-compose.yml)
	for _, v := range []string{"GITLAB_PORT", "REGISTRY_PORT", "KEYCLOAK_PORT", "K8S_HTTP_PORT", "SYMPHONY_PORT"} {
		t.Setenv(v, "8000")
	}
	for _, f := range []string{"../../demo/gitlab/symphony-config/symphony.yaml", "../../demo/symphony.yaml", "../../symphony.yaml"} {
		cfg, err := config.Load(f)
		if err != nil {
			t.Errorf("%s : %v", f, err)
			continue
		}
		if err := validateProviders(cfg.Providers); err != nil {
			t.Errorf("%s : %v", f, err)
		}
		runtimes := map[string]bool{}
		for _, rt := range cfg.Providers.Runtime {
			runtimes[rt.ID] = true
		}
		if err := environments.Validate(cfg.Environments, func(rt string) bool { return runtimes[rt] }); err != nil {
			t.Errorf("%s : %v", f, err)
		}
		if cfg.ConfigSource != nil {
			if err := validateDef("scm", cfg.ConfigSource.SCM); err != nil {
				t.Errorf("%s : config_source.scm : %v", f, err)
			}
		}
	}
}

// TestReferenceCoversSchema : la configuration de référence documente chaque
// type de provider et chacune de ses clés, et chaque section de la
// configuration ; une clé ajoutée à schemas sans être documentée échoue ici
func TestReferenceCoversSchema(t *testing.T) {
	const file = "../../documentation/reference/symphony.yaml"
	cfg, err := config.Load(file)
	if err != nil {
		t.Fatal(err)
	}
	sections := map[string][]config.ProviderDef{
		"scm": cfg.Providers.SCM, "ci": cfg.Providers.CI, "registry": cfg.Providers.Registry,
		"runtime": cfg.Providers.Runtime, "secrets": cfg.Providers.Secrets, "identity": cfg.Providers.Identity,
		"notifications": cfg.Providers.Notifications,
	}
	for kind, types := range schemas {
		for typ, schema := range types {
			i := slices.IndexFunc(sections[kind], func(d config.ProviderDef) bool { return d.Type == typ })
			if i < 0 {
				t.Errorf("providers.%s : type %s absent de %s", kind, typ, file)
				continue
			}
			for _, key := range slices.Concat(schema.required, schema.optional, schema.oneOf) {
				if _, ok := sections[kind][i].Config[key]; !ok {
					t.Errorf("providers.%s %s : clé %s absente de %s", kind, typ, key, file)
				}
			}
		}
	}
	raw, _ := os.ReadFile(file)
	fields := reflect.TypeOf(config.Config{})
	for i := 0; i < fields.NumField(); i++ {
		key := strings.Split(fields.Field(i).Tag.Get("yaml"), ",")[0]
		// templates : ancien nom de stacks, exclusif d'elle, documenté en commentaire
		pattern := `(?m)^` + key + `:`
		if key == "templates" {
			pattern = `(?m)^# templates:`
		}
		if key != "config_source" && !regexp.MustCompile(pattern).Match(raw) {
			t.Errorf("section %s absente de %s", key, file)
		}
	}
}
