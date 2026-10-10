package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yourorg/symphony/internal/provider"
)

type Provider struct {
	id         string
	client     *http.Client
	pullClient *http.Client // pull : peut durer bien plus que 30 s
}

func New(id, socketPath string) *Provider {
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			return net.DialTimeout("unix", socketPath, 5*time.Second)
		},
	}
	return &Provider{
		id:         id,
		client:     &http.Client{Timeout: 30 * time.Second, Transport: transport},
		pullClient: &http.Client{Timeout: 10 * time.Minute, Transport: transport},
	}
}

func (p *Provider) ID() string   { return p.id }
func (p *Provider) Type() string { return "docker" }

func (p *Provider) docker(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "http://localhost/v1.43"+path, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("docker socket: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

// splitImage sépare le dépôt et le tag d'une image. Le tag explicite
// l'emporte sur celui de l'image ; par défaut "latest". Le ":" d'un port de
// registry (gitlab.local:5050/...) n'est pas un tag.
func splitImage(image, tag string) (string, string) {
	repo := image
	if i := strings.LastIndex(image, ":"); i > strings.LastIndex(image, "/") {
		repo = image[:i]
		if tag == "" {
			tag = image[i+1:]
		}
	}
	if tag == "" {
		tag = "latest"
	}
	return repo, tag
}

// pull télécharge l'image. Docker renvoie 200 puis signale une erreur en
// cours de flux : chaque ligne JSON est lue pour la détecter.
func (p *Provider) pull(repo, tag string, auth *provider.RegistryAuth) error {
	req, err := http.NewRequest("POST", "http://localhost/v1.43/images/create?"+url.Values{
		"fromImage": {repo}, "tag": {tag},
	}.Encode(), nil)
	if err != nil {
		return err
	}
	if auth != nil {
		b, _ := json.Marshal(auth)
		req.Header.Set("X-Registry-Auth", base64.URLEncoding.EncodeToString(b))
	}
	resp, err := p.pullClient.Do(req)
	if err != nil {
		return fmt.Errorf("docker socket: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		data, _ := io.ReadAll(resp.Body)
		var e struct {
			Message string `json:"message"`
		}
		if json.Unmarshal(data, &e) == nil && e.Message != "" {
			return fmt.Errorf("docker pull: status %d — %s", resp.StatusCode, e.Message)
		}
		return fmt.Errorf("docker pull: status %d — %s", resp.StatusCode, string(data))
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var msg struct {
			Error string `json:"error"`
		}
		if err := dec.Decode(&msg); err == io.EOF {
			return nil
		} else if err != nil {
			return fmt.Errorf("docker pull: %w", err)
		}
		if msg.Error != "" {
			return fmt.Errorf("docker pull: %s", msg.Error)
		}
	}
}

func (p *Provider) Deploy(req provider.DeployRequest) (*provider.Deployment, error) {
	repo, tag := splitImage(req.Image, req.Tag)
	image := repo + ":" + tag

	// Pull avant de toucher au container en place : si l'image est
	// inaccessible, l'ancien déploiement continue de tourner. Une image
	// présente localement (build local, registry injoignable) suffit.
	if err := p.pull(repo, tag, req.RegistryAuth); err != nil {
		if _, status, _ := p.docker("GET", "/images/"+image+"/json", nil); status != 200 {
			return nil, err
		}
		slog.Warn("pull impossible, image locale utilisée", "image", image, "error", err)
	}

	// Nettoyage préalable
	p.docker("POST", fmt.Sprintf("/containers/%s/stop", req.Name), nil)
	p.docker("DELETE", fmt.Sprintf("/containers/%s?force=true", req.Name), nil)

	port := req.Port
	if port == 0 {
		port = 8080
	}
	portKey := fmt.Sprintf("%d/tcp", port)

	env := make([]string, 0, len(req.EnvVars))
	for k, v := range req.EnvVars {
		env = append(env, k+"="+v)
	}

	restartPolicy := req.RestartPolicy
	if restartPolicy == "" {
		restartPolicy = "unless-stopped"
	}

	payload := map[string]any{
		"Image": image,
		"Env":   env,
		"ExposedPorts": map[string]any{
			portKey: map[string]any{},
		},
		"HostConfig": map[string]any{
			"PortBindings": map[string]any{
				portKey: []map[string]string{{"HostPort": fmt.Sprintf("%d", port)}},
			},
			"RestartPolicy": map[string]string{"Name": restartPolicy},
		},
		"Labels": map[string]string{
			"symphony":         "true",
			"symphony.project": req.Name,
		},
	}

	data, status, err := p.docker("POST",
		fmt.Sprintf("/containers/create?name=%s", req.Name), payload)
	if err != nil {
		return nil, err
	}
	if status != 201 {
		return nil, fmt.Errorf("docker create: status %d — %s", status, string(data))
	}

	var created struct {
		ID string `json:"Id"`
	}
	json.Unmarshal(data, &created)

	_, status, err = p.docker("POST", fmt.Sprintf("/containers/%s/start", created.ID), nil)
	if err != nil {
		return nil, err
	}
	if status != 204 {
		return nil, fmt.Errorf("docker start: status %d", status)
	}

	return &provider.Deployment{
		ID:        created.ID[:12],
		Name:      req.Name,
		Image:     image,
		Status:    "running",
		URL:       fmt.Sprintf("http://localhost:%d", port),
		Port:      port,
		CreatedAt: time.Now(),
	}, nil
}

func (p *Provider) Stop(deploymentID string) error {
	_, status, err := p.docker("POST", fmt.Sprintf("/containers/%s/stop", deploymentID), nil)
	if err != nil {
		return err
	}
	if status != 204 && status != 304 {
		return fmt.Errorf("docker stop: status %d", status)
	}
	return nil
}

func (p *Provider) Restart(deploymentID string) error {
	_, status, err := p.docker("POST", fmt.Sprintf("/containers/%s/restart", deploymentID), nil)
	if err != nil {
		return err
	}
	if status != 204 {
		return fmt.Errorf("docker restart: status %d", status)
	}
	return nil
}

func (p *Provider) Status(deploymentID string) (*provider.Deployment, error) {
	data, status, err := p.docker("GET", fmt.Sprintf("/containers/%s/json", deploymentID), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("docker inspect: status %d", status)
	}

	var info struct {
		ID   string `json:"Id"`
		Name string `json:"Name"`
		State struct {
			Status string `json:"Status"`
		} `json:"State"`
		Config struct {
			Image  string   `json:"Image"`
			Labels map[string]string `json:"Labels"`
		} `json:"Config"`
		HostConfig struct {
			PortBindings map[string][]struct {
				HostPort string `json:"HostPort"`
			} `json:"PortBindings"`
		} `json:"HostConfig"`
	}
	json.Unmarshal(data, &info)

	port := 0
	url := ""
	for _, bindings := range info.HostConfig.PortBindings {
		if len(bindings) > 0 && bindings[0].HostPort != "" {
			fmt.Sscanf(bindings[0].HostPort, "%d", &port)
			url = fmt.Sprintf("http://localhost:%d", port)
			break
		}
	}

	return &provider.Deployment{
		ID:     info.ID[:12],
		Name:   strings.TrimPrefix(info.Name, "/"),
		Image:  info.Config.Image,
		Status: info.State.Status,
		URL:    url,
		Port:   port,
	}, nil
}

func (p *Provider) List() ([]provider.Deployment, error) {
	data, _, err := p.docker("GET", "/containers/json?all=true&filters=%7B%22label%22%3A%5B%22symphony%3Dtrue%22%5D%7D", nil)
	if err != nil {
		return nil, err
	}

	var containers []struct {
		ID    string            `json:"Id"`
		Names []string          `json:"Names"`
		Image string            `json:"Image"`
		State string            `json:"State"`
		Ports []struct {
			PublicPort int    `json:"PublicPort"`
			Type       string `json:"Type"`
		} `json:"Ports"`
		Labels    map[string]string `json:"Labels"`
		Created   int64             `json:"Created"`
	}
	json.Unmarshal(data, &containers)

	deployments := make([]provider.Deployment, 0, len(containers))
	for _, c := range containers {
		name := ""
		if len(c.Names) > 0 {
			name = strings.TrimPrefix(c.Names[0], "/")
		}
		port := 0
		depURL := ""
		for _, po := range c.Ports {
			if po.PublicPort > 0 && po.Type == "tcp" {
				port = po.PublicPort
				depURL = fmt.Sprintf("http://localhost:%d", port)
				break
			}
		}
		deployments = append(deployments, provider.Deployment{
			ID:        c.ID[:12],
			Name:      name,
			Image:     c.Image,
			Status:    c.State,
			URL:       depURL,
			Port:      port,
			CreatedAt: time.Unix(c.Created, 0),
		})
	}
	return deployments, nil
}

func (p *Provider) Logs(deploymentID string, lines int) (string, error) {
	if lines == 0 {
		lines = 100
	}
	data, status, err := p.docker("GET",
		fmt.Sprintf("/containers/%s/logs?stdout=true&stderr=true&tail=%d", deploymentID, lines), nil)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("docker logs: status %d", status)
	}
	return string(data), nil
}
