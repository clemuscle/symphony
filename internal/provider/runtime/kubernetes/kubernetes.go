package kubernetes

import (
	"fmt"
	"time"

	"github.com/yourorg/symphony/internal/provider"
)

// Provider est un stub — implémentation complète prévue post-MVP
type Provider struct {
	id string
}

func New(id string) *Provider {
	return &Provider{id: id}
}

func (p *Provider) ID() string   { return p.id }
func (p *Provider) Type() string { return "kubernetes" }

func (p *Provider) Deploy(req provider.DeployRequest) (*provider.Deployment, error) {
	return nil, fmt.Errorf("kubernetes provider: non implémenté dans le MVP — utilisez docker")
}

func (p *Provider) Stop(deploymentID string) error {
	return fmt.Errorf("kubernetes provider: non implémenté")
}

func (p *Provider) Restart(deploymentID string) error {
	return fmt.Errorf("kubernetes provider: non implémenté")
}

func (p *Provider) Status(deploymentID string) (*provider.Deployment, error) {
	return nil, fmt.Errorf("kubernetes provider: non implémenté")
}

func (p *Provider) List() ([]provider.Deployment, error) {
	return []provider.Deployment{}, nil
}

func (p *Provider) Logs(deploymentID string, lines int) (string, error) {
	return "", fmt.Errorf("kubernetes provider: non implémenté")
}

var _ provider.RuntimeProvider = (*Provider)(nil)
var _ = time.Now // évite l'import inutilisé
