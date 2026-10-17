package provider

import (
	"errors"
	"strings"
	"testing"
)

func TestParseSecretRef(t *testing.T) {
	cases := []struct {
		in   string
		want SecretRef
		err  bool
	}{
		{"secret://vault/symphony/gitlab#token", SecretRef{"vault", "symphony/gitlab", "token"}, false},
		{"secret://env/GITLAB_TOKEN", SecretRef{"env", "GITLAB_TOKEN", ""}, false},
		{"secret://vault//a/b/#k", SecretRef{"vault", "a/b", "k"}, false},
		{"secret://vault", SecretRef{}, true},
		{"secret:///chemin", SecretRef{}, true},
		{"vault/chemin", SecretRef{}, true},
	}
	for _, c := range cases {
		got, err := ParseSecretRef(c.in)
		if (err != nil) != c.err || got != c.want {
			t.Errorf("ParseSecretRef(%q) = %+v, %v ; attendu %+v (erreur : %v)", c.in, got, err, c.want, c.err)
		}
	}
	if s := (SecretRef{"vault", "a/b", "k"}).String(); s != "secret://vault/a/b#k" {
		t.Errorf("String() = %s", s)
	}
}

type mapSecrets map[string]string

func (m mapSecrets) ID() string             { return "fake" }
func (m mapSecrets) Type() string           { return "fake" }
func (m mapSecrets) Check() (string, error) { return "", nil }
func (m mapSecrets) Get(path, key string) (string, error) {
	v, ok := m[path+"#"+key]
	if !ok {
		return "", ErrNotFound
	}
	return v, nil
}

func TestResolveSecret(t *testing.T) {
	lookup := func(id string) (SecretsProvider, error) {
		if id != "fake" {
			return nil, errors.New("provider inconnu")
		}
		return mapSecrets{"app/db#password": "s3cr3t"}, nil
	}
	if v, err := ResolveSecret("valeur simple", lookup); err != nil || v != "valeur simple" {
		t.Errorf("valeur simple modifiée : %q, %v", v, err)
	}
	if v, err := ResolveSecret("secret://fake/app/db#password", lookup); err != nil || v != "s3cr3t" {
		t.Errorf("secret = %q, %v", v, err)
	}
	_, err := ResolveSecret("secret://fake/app/db#absent", lookup)
	if !errors.Is(err, ErrNotFound) || !strings.Contains(err.Error(), "secret://fake/app/db#absent") {
		t.Errorf("erreur = %v : doit nommer la référence et envelopper ErrNotFound", err)
	}
	if _, err := ResolveSecret("secret://autre/x", lookup); err == nil {
		t.Error("provider inconnu accepté")
	}
}
