package docker

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"time"

	"github.com/yourorg/symphony/internal/providers"
)

type Provider struct {
	client *http.Client
}

func New(socketPath string) (*Provider, error) {
	if socketPath == "" {
		return nil, fmt.Errorf("docker: socketPath is required")
	}
	return &Provider{
		client: &http.Client{
			Timeout: 30 * time.Second,
			Transport: &http.Transport{
				DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
					return net.DialTimeout("unix", socketPath, 5*time.Second)
				},
			},
		},
	}, nil
}

// Ping vérifie que le daemon Docker est accessible. Utilisé par le wizard
// de configuration pour valider la connectivité avant de sauvegarder.
func (p *Provider) Ping() error {
	req, err := http.NewRequest("GET", "http://localhost/v1.43/info", nil)
	if err != nil {
		return fmt.Errorf("docker ping: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("docker ping: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("docker ping: status %d", resp.StatusCode)
	}
	return nil
}

// ListContainers retourne les containers en cours d'exécution via l'API Docker.
// Lecture seule — jamais utilisé pour provisionner ou détruire.
func (p *Provider) ListContainers() ([]providers.ContainerInfo, error) {
	req, err := http.NewRequest("GET", "http://localhost/v1.43/containers/json", nil)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("docker: %w", err)
	}
	defer resp.Body.Close()

	var raw []struct {
		ID      string   `json:"Id"`
		Names   []string `json:"Names"`
		Image   string   `json:"Image"`
		Status  string   `json:"Status"`
		Created int64    `json:"Created"`
		Ports   []struct {
			PublicPort  int `json:"PublicPort"`
			PrivatePort int `json:"PrivatePort"`
		} `json:"Ports"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, fmt.Errorf("docker: parse containers: %w", err)
	}

	out := make([]providers.ContainerInfo, 0, len(raw))
	for _, c := range raw {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		port := 0
		for _, p := range c.Ports {
			if p.PublicPort > 0 {
				port = p.PublicPort
				break
			}
		}
		out = append(out, providers.ContainerInfo{
			ID:      c.ID[:min(12, len(c.ID))],
			Name:    name,
			Image:   c.Image,
			Status:  c.Status,
			Port:    port,
			Created: c.Created,
		})
	}
	return out, nil
}

func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
