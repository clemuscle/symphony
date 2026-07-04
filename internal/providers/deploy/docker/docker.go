package docker

import (
	"bytes"
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

func (p *Provider) docker(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("docker %s %s: marshal request: %w", method, path, err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://localhost/v1.43"+path, buf)
	if err != nil {
		return nil, 0, fmt.Errorf("docker %s %s: build request: %w", method, path, err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("docker %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("docker %s %s: read response: %w", method, path, err)
	}
	return data, resp.StatusCode, nil
}

func (p *Provider) Stop(deploymentID string) error {
	_, _, err := p.docker("POST", fmt.Sprintf("/containers/%s/stop", deploymentID), nil)
	return err
}

func (p *Provider) Status(deploymentID string) (string, error) {
	data, _, err := p.docker("GET", fmt.Sprintf("/containers/%s/json", deploymentID), nil)
	if err != nil {
		return "", err
	}
	var result struct {
		State struct{ Status string `json:"Status"` } `json:"State"`
	}
	if err := json.Unmarshal(data, &result); err != nil {
		return "", fmt.Errorf("docker status %s: decode response: %w", deploymentID, err)
	}
	return result.State.Status, nil
}

func (p *Provider) List() ([]providers.Deployment, error) {
	data, _, err := p.docker("GET", "/containers/json?all=true", nil)
	if err != nil {
		return nil, err
	}
	var containers []struct {
		ID    string   `json:"Id"`
		Image string   `json:"Image"`
		Names []string `json:"Names"`
		State string   `json:"State"`
		Ports []struct {
			PublicPort int `json:"PublicPort"`
		} `json:"Ports"`
	}
	if err := json.Unmarshal(data, &containers); err != nil {
		return nil, fmt.Errorf("docker list: decode response: %w", err)
	}

	deployments := make([]providers.Deployment, 0, len(containers))
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		deployURL := ""
		if len(c.Ports) > 0 && c.Ports[0].PublicPort > 0 {
			deployURL = fmt.Sprintf("http://localhost:%d", c.Ports[0].PublicPort)
		}
		deployments = append(deployments, providers.Deployment{
			ID:          c.ID[:12],
			ProjectName: name,
			Image:       c.Image,
			Status:      c.State,
			URL:         deployURL,
		})
	}
	return deployments, nil
}
