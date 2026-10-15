package main

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"log/slog"
	"strconv"

	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/instrumented"
	envsecrets "github.com/clemuscle/symphony/internal/provider/secrets/env"
	vaultsecrets "github.com/clemuscle/symphony/internal/provider/secrets/vault"
	"gopkg.in/yaml.v3"
)

// envSecretsID : provider de secrets toujours présent (variables d'environnement)
const envSecretsID = "env"

// registerSecrets crée les providers de secrets dans providers. Chacun peut
// prendre ses propres identifiants dans un provider déclaré avant lui (ou
// dans env). Contrairement aux autres types, un provider de secrets en échec
// fait refuser la configuration : les références vers lui échoueraient.
func registerSecrets(providers *provider.Registry, defs []config.ProviderDef) error {
	providers.RegisterSecrets(instrumented.Secrets(envsecrets.New(envSecretsID)), false)
	for _, def := range defs {
		if def.ID == "" || def.ID == envSecretsID {
			return fmt.Errorf("provider de secrets : id vide ou réservé (%q)", def.ID)
		}
		resolved, err := resolveDef(def, providers.Secrets)
		if err != nil {
			return fmt.Errorf("provider de secrets %s: %w", def.ID, err)
		}
		p, err := newSecrets(resolved)
		if err != nil {
			return fmt.Errorf("provider de secrets %s: %w", def.ID, err)
		}
		providers.RegisterSecrets(instrumented.Secrets(p), def.Default)
	}
	return nil
}

// newSecrets crée un provider de secrets à partir de sa définition (résolue)
func newSecrets(def config.ProviderDef) (provider.SecretsProvider, error) {
	c := def.Config
	switch def.Type {
	case "env":
		return envsecrets.New(def.ID), nil
	case "vault":
		kv := 0
		if v := c["kv_version"]; v != "" {
			n, err := strconv.Atoi(v)
			if err != nil {
				return nil, fmt.Errorf("kv_version %q invalide", v)
			}
			kv = n
		}
		return vaultsecrets.New(def.ID, vaultsecrets.Config{
			URL:          c["url"],
			Namespace:    c["namespace"],
			Mount:        c["mount"],
			KVVersion:    kv,
			Token:        secretValue(def, "token"),
			RoleID:       secretValue(def, "role_id"),
			SecretID:     secretValue(def, "secret_id"),
			AppRoleMount: c["approle_mount"],
		})
	}
	return nil, fmt.Errorf("type de provider de secrets %q inconnu", def.Type)
}

// resolveConfig renvoie une copie de cfg où chaque référence secret:// des
// providers (hors secrets, déjà résolus) et des webhooks est remplacée par le
// secret. La moindre référence introuvable fait échouer l'ensemble : une
// configuration à moitié résolue retirerait des providers qui marchaient.
func resolveConfig(cfg *config.Config, lookup provider.SecretLookup) (*config.Config, error) {
	out := *cfg
	lists := []struct {
		kind string
		defs *[]config.ProviderDef
	}{
		{"scm", &out.Providers.SCM},
		{"ci", &out.Providers.CI},
		{"registry", &out.Providers.Registry},
		{"runtime", &out.Providers.Runtime},
		{"identity", &out.Providers.Identity},
	}
	for _, l := range lists {
		resolved := make([]config.ProviderDef, len(*l.defs))
		for i, def := range *l.defs {
			r, err := resolveDef(def, lookup)
			if err != nil {
				return nil, fmt.Errorf("provider %s %s: %w", l.kind, def.ID, err)
			}
			resolved[i] = r
		}
		*l.defs = resolved
	}
	if cfg.Webhooks != nil {
		w := *cfg.Webhooks
		s, err := provider.ResolveSecret(w.Secret, lookup)
		if err != nil {
			return nil, fmt.Errorf("webhooks.secret: %w", err)
		}
		w.Secret = s
		out.Webhooks = &w
	}
	return &out, nil
}

// resolveDef : copie de def, références secret:// remplacées
func resolveDef(def config.ProviderDef, lookup provider.SecretLookup) (config.ProviderDef, error) {
	c := make(map[string]string, len(def.Config))
	for k, v := range def.Config {
		s, err := provider.ResolveSecret(v, lookup)
		if err != nil {
			return def, fmt.Errorf("%s: %w", k, err)
		}
		c[k] = s
	}
	def.Config = c
	return def, nil
}

// fingerprint résume une configuration résolue : il change quand un secret
// change dans son provider (rotation), alors que le fichier, lui, est le même
func fingerprint(resolved *config.Config) string {
	data, _ := yaml.Marshal(resolved)
	sum := sha256.Sum256(data)
	return hex.EncodeToString(sum[:])
}

// legacyAuth convertit l'ancienne section auth (issuer, client_id…) en
// provider identity de type oidc, d'id "oidc" ; warn : le signaler (pas à
// chaque relecture des secrets)
func legacyAuth(cfg *config.Config, warn bool) *config.Config {
	a := cfg.Auth
	if a == nil || a.Issuer == "" {
		return cfg
	}
	if warn {
		slog.Warn("auth.issuer et suivants sont dépréciés : déclarer un provider identity et le désigner par auth.identity")
	}
	out := *cfg
	out.Providers.Identity = append([]config.ProviderDef{{
		ID:   "oidc",
		Type: "oidc",
		Config: map[string]string{
			"issuer":            a.Issuer,
			"discovery_url":     a.DiscoveryURL,
			"client_id":         a.ClientID,
			"client_secret_env": a.ClientSecretEnv,
			"redirect_url":      a.RedirectURL,
			"groups_claim":      a.GroupsClaim,
		},
	}}, cfg.Providers.Identity...)
	auth := *a
	auth.Identity = "oidc"
	out.Auth = &auth
	return &out
}
