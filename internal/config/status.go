package config

import (
	"crypto/sha256"
	"encoding/hex"
	"time"
)

// Status : la configuration en place et ses derniers rechargements, pour la
// page Administration. Les messages d'erreur ne contiennent pas de secret :
// les références secret:// y sont nommées, jamais leur valeur.
type Status struct {
	// Source : le fichier local, ou le repo, la branche et le chemin lus
	Source string `json:"source"`
	// Revision : empreinte du contenu en place (les 12 premiers caractères de
	// son SHA-256), à comparer avec Refused.Revision
	Revision  string    `json:"revision"`
	AppliedAt time.Time `json:"applied_at"`
	// CheckedAt : dernière lecture de la source, réussie ou non
	CheckedAt time.Time `json:"checked_at"`
	// ReadError : la source est illisible (forge injoignable, jeton refusé)
	ReadError string `json:"read_error,omitempty"`
	// SecretsError : la relecture des secrets (rotation) échoue
	SecretsError string `json:"secrets_error,omitempty"`
	// Refused : la version présente dans la source a été refusée, celle en
	// place reste appliquée
	Refused *Refusal `json:"refused,omitempty"`
	// Warnings : la configuration en place est appliquée, mais à corriger
	// (ancien nom de section…)
	Warnings []string `json:"warnings"`
	// Types : type de chaque provider en place, par <section>/<id>
	Types map[string]string `json:"-"`
}

// Refusal : une version de la configuration refusée, et pourquoi
type Refusal struct {
	Revision string    `json:"revision"`
	At       time.Time `json:"at"`
	Error    string    `json:"error"`
}

// Revision : empreinte courte d'un contenu de configuration
func Revision(raw []byte) string {
	sum := sha256.Sum256(raw)
	return hex.EncodeToString(sum[:])[:12]
}

// ProviderTypes : type de chaque provider de la configuration, par
// <section>/<id> ; le provider de secrets env existe toujours
func (c *Config) ProviderTypes() map[string]string {
	types := map[string]string{"secrets/env": "env"}
	add := func(kind string, defs []ProviderDef) {
		for _, d := range defs {
			types[kind+"/"+d.ID] = d.Type
		}
	}
	add("scm", c.Providers.SCM)
	add("ci", c.Providers.CI)
	add("registry", c.Providers.Registry)
	add("runtime", c.Providers.Runtime)
	add("secrets", c.Providers.Secrets)
	add("identity", c.Providers.Identity)
	add("notifications", c.Providers.Notifications)
	return types
}
