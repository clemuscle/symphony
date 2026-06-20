package gitlabregistry

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/yourorg/symphony/internal/providers"
)

type Provider struct {
	BaseURL     string
	RegistryURL string
	Token       string
	client      *http.Client
}

func New(baseURL, registryURL, token string) (*Provider, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("gitlabregistry: baseURL is required")
	}
	if registryURL == "" {
		return nil, fmt.Errorf("gitlabregistry: registryURL is required")
	}
	if token == "" {
		return nil, fmt.Errorf("gitlabregistry: token is required")
	}
	return &Provider{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		RegistryURL: registryURL,
		Token:       token,
		client:      &http.Client{Timeout: 15 * time.Second},
	}, nil
}

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, fmt.Errorf("gitlabregistry %s %s: marshal request: %w", method, path, err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.BaseURL+"/api/v4"+path, buf)
	if err != nil {
		return nil, 0, fmt.Errorf("gitlabregistry %s %s: build request: %w", method, path, err)
	}
	req.Header.Set("PRIVATE-TOKEN", p.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gitlabregistry %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, fmt.Errorf("gitlabregistry %s %s: read response: %w", method, path, err)
	}
	return data, resp.StatusCode, nil
}

func (p *Provider) GetRegistryURL(projectPath string) (string, error) {
	encoded := url.PathEscape(projectPath)
	data, status, err := p.api("GET", fmt.Sprintf("/projects/%s/registry/repositories", encoded), nil)
	if err != nil {
		return "", err
	}
	if status < 200 || status >= 300 {
		return "", fmt.Errorf("gitlabregistry getRegistryURL %s: %d — %s", projectPath, status, string(data))
	}
	return fmt.Sprintf("%s/%s", p.RegistryURL, projectPath), nil
}

func (p *Provider) ListImages(projectPath string) ([]providers.Image, error) {
	encoded := url.PathEscape(projectPath)
	data, status, err := p.api("GET", fmt.Sprintf("/projects/%s/registry/repositories?tags=true", encoded), nil)
	if err != nil {
		return nil, err
	}
	if status < 200 || status >= 300 {
		return nil, fmt.Errorf("gitlabregistry listImages %s: %d — %s", projectPath, status, string(data))
	}

	var repos []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, fmt.Errorf("gitlabregistry listImages %s: decode response: %w", projectPath, err)
	}

	var images []providers.Image
	for _, r := range repos {
		for _, t := range r.Tags {
			images = append(images, providers.Image{
				Name: r.Name,
				Tag:  t.Name,
			})
		}
	}
	return images, nil
}

// resolveRepositoryID retrouve l'ID de repository du registre GitLab
// correspondant au projet, requis par l'API de suppression de tag.
func (p *Provider) resolveRepositoryID(projectPath string) (int, error) {
	encoded := url.PathEscape(projectPath)
	data, status, err := p.api("GET", fmt.Sprintf("/projects/%s/registry/repositories", encoded), nil)
	if err != nil {
		return 0, err
	}
	if status < 200 || status >= 300 {
		return 0, fmt.Errorf("gitlabregistry resolveRepositoryID %s: %d — %s", projectPath, status, string(data))
	}

	var repos []struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(data, &repos); err != nil {
		return 0, fmt.Errorf("gitlabregistry resolveRepositoryID %s: decode response: %w", projectPath, err)
	}
	if len(repos) == 0 {
		return 0, fmt.Errorf("gitlabregistry resolveRepositoryID %s: no registry repository found", projectPath)
	}
	return repos[0].ID, nil
}

func (p *Provider) DeleteImage(projectPath, tag string) error {
	encoded := url.PathEscape(projectPath)
	repositoryID, err := p.resolveRepositoryID(projectPath)
	if err != nil {
		return fmt.Errorf("gitlabregistry deleteImage %s:%s: %w", projectPath, tag, err)
	}

	data, status, err := p.api("DELETE",
		fmt.Sprintf("/projects/%s/registry/repositories/%d/tags/%s", encoded, repositoryID, url.PathEscape(tag)),
		nil)
	if err != nil {
		return err
	}
	if status != http.StatusOK && status != http.StatusNoContent {
		return fmt.Errorf("gitlabregistry deleteImage %s:%s: %d — %s", projectPath, tag, status, string(data))
	}
	return nil
}
