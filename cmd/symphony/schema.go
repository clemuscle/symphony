package main

import (
	"errors"
	"fmt"
	"slices"
	"sort"
	"strings"

	"github.com/clemuscle/symphony/internal/config"
)

// providerSchema : clés de config d'un type de provider. Une clé inconnue
// ou une clé requise absente fait refuser la configuration ; oneOf : au moins
// une de ces clés (ex. un jeton, ou un SCM dont reprendre le jeton).
type providerSchema struct {
	required []string
	optional []string
	oneOf    []string
}

// schemas : types de provider connus, par section de la configuration
var schemas = map[string]map[string]providerSchema{
	"scm": {
		"gitlab": {required: []string{"url", "token"}, optional: []string{"namespace"}},
		"github": {required: []string{"token"}, optional: []string{"org"}},
	},
	"ci": {
		"gitlab_ci":      {required: []string{"url"}, oneOf: []string{"token", "scm_ref"}},
		"github_actions": {oneOf: []string{"token", "scm_ref"}},
	},
	"registry": {
		"gitlab_registry": {required: []string{"url", "registry_host"}, oneOf: []string{"token", "scm_ref"}},
		"ghcr":            {optional: []string{"owner"}, oneOf: []string{"token", "scm_ref"}},
	},
	"runtime": {
		"docker": {optional: []string{"socket", "health_timeout"}},
		"argocd": {
			required: []string{"url", "token", "repo", "repo_url"},
			optional: []string{"scm_ref", "insecure", "branch", "path", "project", "cluster",
				"namespace", "ingress_domain", "ingress_class", "ingress_port"},
		},
	},
	"secrets": {
		"vault": {
			required: []string{"url"},
			optional: []string{"namespace", "mount", "kv_version", "token", "role_id", "secret_id", "approle_mount"},
		},
		"env": {},
	},
	"identity": {
		"oidc": {
			required: []string{"issuer", "client_id"},
			optional: []string{"discovery_url", "client_secret", "redirect_url", "groups_claim"},
		},
		"keycloak": {
			required: []string{"url", "realm", "client_id"},
			optional: []string{"public_url", "client_secret", "redirect_url", "groups_claim",
				"directory_client_id", "directory_client_secret"},
		},
	},
}

// validateProviders vérifie chaque définition de la configuration : id
// présent et unique dans sa section, type connu, clés connues et complètes.
// Toutes les erreurs sont rapportées ensemble.
func validateProviders(p config.ProvidersConfig) error {
	sections := []struct {
		kind string
		defs []config.ProviderDef
	}{
		{"secrets", p.Secrets}, {"scm", p.SCM}, {"ci", p.CI}, {"registry", p.Registry},
		{"runtime", p.Runtime}, {"identity", p.Identity},
	}
	var errs []error
	for _, s := range sections {
		seen := map[string]bool{}
		for i, def := range s.defs {
			where := fmt.Sprintf("providers.%s[%d]", s.kind, i)
			if def.ID != "" {
				where = fmt.Sprintf("providers.%s %s", s.kind, def.ID)
			}
			if def.ID == "" {
				errs = append(errs, fmt.Errorf("%s : id requis", where))
			} else if seen[def.ID] {
				errs = append(errs, fmt.Errorf("%s : id en double", where))
			}
			seen[def.ID] = true
			if err := validateDef(s.kind, def); err != nil {
				errs = append(errs, fmt.Errorf("%s : %w", where, err))
			}
		}
	}
	return errors.Join(errs...)
}

// validateDef vérifie une définition contre le schéma de son type
func validateDef(kind string, def config.ProviderDef) error {
	types := schemas[kind]
	schema, ok := types[def.Type]
	if !ok {
		return fmt.Errorf("type %q inconnu (%s)", def.Type, strings.Join(sortedKeys(types), ", "))
	}
	allowed := slices.Concat(schema.required, schema.optional, schema.oneOf)
	var unknown, missing []string
	for k := range def.Config {
		if !slices.Contains(allowed, k) {
			unknown = append(unknown, k)
		}
	}
	for _, k := range schema.required {
		if def.Config[k] == "" {
			missing = append(missing, k)
		}
	}
	sort.Strings(unknown)
	var errs []error
	if len(unknown) > 0 {
		hint := ""
		if slices.ContainsFunc(unknown, func(k string) bool { return strings.HasSuffix(k, "_env") }) {
			hint = " ; une variable d'environnement s'écrit secret://env/<VARIABLE>"
		}
		errs = append(errs, fmt.Errorf("clé(s) inconnue(s) %s (connues : %s)%s",
			strings.Join(unknown, ", "), strings.Join(sorted(allowed), ", "), hint))
	}
	if len(missing) > 0 {
		errs = append(errs, fmt.Errorf("clé(s) requise(s) absente(s) : %s", strings.Join(missing, ", ")))
	}
	if len(schema.oneOf) > 0 && !slices.ContainsFunc(schema.oneOf, func(k string) bool { return def.Config[k] != "" }) {
		errs = append(errs, fmt.Errorf("l'une de ces clés est requise : %s", strings.Join(schema.oneOf, ", ")))
	}
	return errors.Join(errs...)
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func sorted(s []string) []string {
	out := slices.Clone(s)
	sort.Strings(out)
	return out
}
