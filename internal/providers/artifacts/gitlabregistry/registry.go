package gitlabregistry

import (
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

func New(baseURL, registryURL, token string) *Provider {
	return &Provider{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		RegistryURL: registryURL,
		Token:       token,
		client:      &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) api(path string) ([]byte, error) {
	req, _ := http.NewRequest("GET", p.BaseURL+"/api/v4"+path, nil)
	req.Header.Set("PRIVATE-TOKEN", p.Token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}

func (p *Provider) GetRegistryURL(projectPath string) (string, error) {
	return fmt.Sprintf("%s/%s", p.RegistryURL, projectPath), nil
}

func (p *Provider) ListImages(projectPath string) ([]providers.Image, error) {
	encoded := url.PathEscape(projectPath)
	data, err := p.api(fmt.Sprintf("/projects/%s/registry/repositories", encoded))
	if err != nil {
		return nil, err
	}

	var repos []struct {
		ID   int    `json:"id"`
		Name string `json:"name"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	json.Unmarshal(data, &repos)

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

func (p *Provider) DeleteImage(projectPath, tag string) error {
	// implémentation future
	return fmt.Errorf("not implemented")
}
