package docker

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
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

	containerPort := req.Port
	if containerPort == 0 {
		containerPort = 8080
	}
	portKey := fmt.Sprintf("%d/tcp", containerPort)

	// Port hôte : demandé, sinon celui du container remplacé (l'URL ne change
	// pas d'un déploiement à l'autre), sinon "" : Docker en choisit un libre
	old, err := p.inspect(req.Name)
	if err != nil && !errors.Is(err, provider.ErrNotFound) {
		return nil, err
	}
	hostPort := ""
	if req.HostPort != 0 {
		hostPort = strconv.Itoa(req.HostPort)
	} else if old != nil {
		if port := old.hostPort(); port != 0 {
			hostPort = strconv.Itoa(port)
		}
	}

	// Le nouveau container est créé à côté de l'ancien, sous un nom
	// temporaire, et ne prend le nom définitif qu'une fois démarré
	name := req.Name
	if old != nil {
		name = req.Name + nextSuffix
		p.docker("DELETE", fmt.Sprintf("/containers/%s?force=true", name), nil) // reste d'un échec précédent
	}

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
				portKey: []map[string]string{{"HostPort": hostPort}},
			},
			"RestartPolicy": map[string]string{"Name": restartPolicy},
		},
		"Labels": map[string]string{
			"symphony":         "true",
			"symphony.project": req.Name,
		},
	}

	data, status, err := p.docker("POST",
		fmt.Sprintf("/containers/create?name=%s", name), payload)
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

	// Bascule : l'ancien est arrêté (il libère le port hôte) mais conservé
	// jusqu'à ce que le nouveau ait tenu startupGrace
	oldRunning := old != nil && old.State.Status == "running"
	if oldRunning {
		p.docker("POST", fmt.Sprintf("/containers/%s/stop", old.ID), nil)
	}
	if err := p.startAndWatch(created.ID); err != nil {
		p.docker("DELETE", fmt.Sprintf("/containers/%s?force=true", created.ID), nil)
		if oldRunning {
			if _, st, _ := p.docker("POST", fmt.Sprintf("/containers/%s/start", old.ID), nil); st != 204 && st != 304 {
				slog.Error("rollback : l'ancien container ne redémarre pas", "container", req.Name, "status", st)
				return nil, fmt.Errorf("%w (et l'ancien container n'a pas pu être redémarré)", err)
			}
		}
		return nil, fmt.Errorf("%w (ancien déploiement conservé)", err)
	}

	if old != nil {
		p.docker("DELETE", fmt.Sprintf("/containers/%s?force=true", old.ID), nil)
		if _, st, err := p.docker("POST",
			fmt.Sprintf("/containers/%s/rename?name=%s", created.ID, url.QueryEscape(req.Name)), nil); err != nil || st != 204 {
			// Le nouveau tourne : le déploiement a réussi, seul le nom est provisoire
			slog.Warn("renommage du container", "from", name, "to", req.Name, "status", st, "error", err)
		}
	}

	// Port réellement publié (choisi par Docker si hostPort est vide)
	return p.Status(created.ID)
}

// nextSuffix : nom temporaire du nouveau container pendant la bascule
const nextSuffix = "-next"

// startupGrace : durée pendant laquelle un nouveau container doit rester
// "running" avant de remplacer l'ancien (attrape les crashs au démarrage)
var startupGrace = 3 * time.Second

// startAndWatch démarre le container et vérifie qu'il tourne toujours après
// startupGrace. En cas d'échec, l'erreur contient la fin de ses logs.
func (p *Provider) startAndWatch(id string) error {
	data, status, err := p.docker("POST", fmt.Sprintf("/containers/%s/start", id), nil)
	if err != nil {
		return err
	}
	if status != 204 {
		// Port hôte déjà pris, typiquement
		return fmt.Errorf("docker start: status %d — %s", status, strings.TrimSpace(string(data)))
	}
	time.Sleep(startupGrace)
	info, err := p.inspect(id)
	if err != nil {
		return err
	}
	if info.State.Status != "running" {
		logs, _ := p.Logs(id, 20)
		return fmt.Errorf("le container ne tient pas au démarrage (état %s) — logs : %s",
			info.State.Status, strings.TrimSpace(logs))
	}
	return nil
}

// cleanLogs retire l'en-tête binaire de 8 octets que Docker ajoute à chaque
// ligne des logs d'un container sans TTY
func cleanLogs(raw string) string {
	var b strings.Builder
	for len(raw) >= 8 && (raw[0] == 1 || raw[0] == 2) {
		size := int(raw[4])<<24 | int(raw[5])<<16 | int(raw[6])<<8 | int(raw[7])
		raw = raw[8:]
		if size > len(raw) {
			size = len(raw)
		}
		b.WriteString(raw[:size])
		raw = raw[size:]
	}
	b.WriteString(raw)
	return b.String()
}

// containerInfo : sous-ensemble de GET /containers/{id}/json
type containerInfo struct {
	ID      string    `json:"Id"`
	Name    string    `json:"Name"`
	Created time.Time `json:"Created"`
	State   struct {
		Status string `json:"Status"`
	} `json:"State"`
	Config struct {
		Image  string            `json:"Image"`
		Labels map[string]string `json:"Labels"`
	} `json:"Config"`
	HostConfig struct {
		PortBindings map[string][]portBinding `json:"PortBindings"`
	} `json:"HostConfig"`
	NetworkSettings struct {
		Ports map[string][]portBinding `json:"Ports"`
	} `json:"NetworkSettings"`
}

type portBinding struct {
	HostPort string `json:"HostPort"`
}

// hostPort : port publié du container en marche, sinon celui demandé à sa
// création (un container arrêté n'a plus de NetworkSettings.Ports)
func (c *containerInfo) hostPort() int {
	for _, ports := range []map[string][]portBinding{c.NetworkSettings.Ports, c.HostConfig.PortBindings} {
		for _, bindings := range ports {
			for _, b := range bindings {
				if port, err := strconv.Atoi(b.HostPort); err == nil && port > 0 {
					return port
				}
			}
		}
	}
	return 0
}

func (p *Provider) inspect(idOrName string) (*containerInfo, error) {
	data, status, err := p.docker("GET", fmt.Sprintf("/containers/%s/json", idOrName), nil)
	if err != nil {
		return nil, err
	}
	if status == 404 {
		return nil, provider.ErrNotFound
	}
	if status != 200 {
		return nil, fmt.Errorf("docker inspect: status %d", status)
	}
	var info containerInfo
	if err := json.Unmarshal(data, &info); err != nil {
		return nil, fmt.Errorf("docker inspect: %w", err)
	}
	return &info, nil
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
	info, err := p.inspect(deploymentID)
	if err != nil {
		return nil, err
	}
	d := &provider.Deployment{
		ID:        info.ID[:12],
		Name:      strings.TrimPrefix(info.Name, "/"),
		Image:     info.Config.Image,
		Status:    info.State.Status,
		Port:      info.hostPort(),
		CreatedAt: info.Created,
	}
	if d.Port != 0 {
		d.URL = fmt.Sprintf("http://localhost:%d", d.Port)
	}
	return d, nil
}

func (p *Provider) List() ([]provider.Deployment, error) {
	data, _, err := p.docker("GET", "/containers/json?all=true&filters=%7B%22label%22%3A%5B%22symphony%3Dtrue%22%5D%7D", nil)
	if err != nil {
		return nil, err
	}

	var containers []struct {
		ID    string   `json:"Id"`
		Names []string `json:"Names"`
		Image string   `json:"Image"`
		State string   `json:"State"`
		Ports []struct {
			PublicPort int    `json:"PublicPort"`
			Type       string `json:"Type"`
		} `json:"Ports"`
		Labels  map[string]string `json:"Labels"`
		Created int64             `json:"Created"`
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
	return cleanLogs(string(data)), nil
}
