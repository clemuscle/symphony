package provider

import (
	"fmt"
	"strings"
)

// SecretScheme préfixe une valeur de configuration lue chez un provider de
// secrets : secret://<provider>/<path>#<key>
const SecretScheme = "secret://"

// SecretRef : référence à un secret, décodée
type SecretRef struct {
	Provider string // id du provider de secrets
	Path     string
	Key      string // vide : clé par défaut du provider
}

func (r SecretRef) String() string {
	s := SecretScheme + r.Provider + "/" + r.Path
	if r.Key != "" {
		s += "#" + r.Key
	}
	return s
}

// IsSecretRef indique si value est une référence secret://
func IsSecretRef(value string) bool { return strings.HasPrefix(value, SecretScheme) }

// ParseSecretRef décode secret://<provider>/<path>[#<key>]
func ParseSecretRef(value string) (SecretRef, error) {
	rest, ok := strings.CutPrefix(value, SecretScheme)
	if !ok {
		return SecretRef{}, fmt.Errorf("référence de secret %q : %s attendu", value, SecretScheme)
	}
	rest, key, _ := strings.Cut(rest, "#")
	id, path, _ := strings.Cut(rest, "/")
	path = strings.Trim(path, "/")
	if id == "" || path == "" {
		return SecretRef{}, fmt.Errorf("référence de secret %q : secret://<provider>/<chemin>[#<clé>] attendu", value)
	}
	return SecretRef{Provider: id, Path: path, Key: key}, nil
}

// SecretLookup trouve un provider de secrets par id
type SecretLookup func(id string) (SecretsProvider, error)

// ResolveSecret renvoie value telle quelle, ou le secret qu'elle référence.
// L'erreur nomme la référence, jamais la valeur.
func ResolveSecret(value string, lookup SecretLookup) (string, error) {
	if !IsSecretRef(value) {
		return value, nil
	}
	ref, err := ParseSecretRef(value)
	if err != nil {
		return "", err
	}
	p, err := lookup(ref.Provider)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ref, err)
	}
	v, err := p.Get(ref.Path, ref.Key)
	if err != nil {
		return "", fmt.Errorf("%s: %w", ref, err)
	}
	return v, nil
}
