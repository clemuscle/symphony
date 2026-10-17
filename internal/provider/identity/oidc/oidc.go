// Package oidc : provider d'identité OIDC générique (Keycloak, GitLab, Dex,
// Entra ID…). Il ne sert qu'à la connexion : pas d'annuaire.
package oidc

import (
	"errors"
	"net/http"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

type Provider struct {
	id       string
	settings provider.OIDCSettings
}

func New(id string, s provider.OIDCSettings) (*Provider, error) {
	if s.Issuer == "" || s.ClientID == "" {
		return nil, errors.New("issuer et client_id requis")
	}
	return &Provider{id: id, settings: s}, nil
}

func (p *Provider) ID() string                  { return p.id }
func (p *Provider) Type() string                { return "oidc" }
func (p *Provider) OIDC() provider.OIDCSettings { return p.settings }

func (p *Provider) ListGroups() ([]provider.Group, error) { return nil, provider.ErrUnsupported }

// Check : la découverte OIDC répond et annonce l'issuer configuré
func (p *Provider) Check() (string, error) {
	return provider.CheckOIDC(&http.Client{Timeout: 10 * time.Second}, p.settings)
}
