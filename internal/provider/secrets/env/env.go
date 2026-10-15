// Package env : provider de secrets qui lit les variables d'environnement
// (secret://env/GITLAB_TOKEN). Toujours disponible sous l'id "env" : c'est
// l'équivalent des clés token_env de la configuration.
package env

import (
	"fmt"
	"os"

	"github.com/clemuscle/symphony/internal/provider"
)

type Provider struct{ id string }

func New(id string) *Provider { return &Provider{id: id} }

func (p *Provider) ID() string   { return p.id }
func (p *Provider) Type() string { return "env" }

// Get lit la variable path ; key n'a pas de sens ici et doit être vide.
// Une variable vide compte comme absente.
func (p *Provider) Get(path, key string) (string, error) {
	if key != "" {
		return "", fmt.Errorf("pas de clé pour une variable d'environnement (#%s)", key)
	}
	v := os.Getenv(path)
	if v == "" {
		return "", fmt.Errorf("variable %s: %w", path, provider.ErrNotFound)
	}
	return v, nil
}
