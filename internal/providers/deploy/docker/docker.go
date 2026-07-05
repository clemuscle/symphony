package docker

import (
	"context"
	"fmt"
	"io"
	"net"
	"net/http"
	"time"
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
