package gitlabregistry

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

// Provider : container registry intégré à GitLab. Les images d'un projet
// sont publiées sous <registry_host>/<namespace>/<projet>.
type Provider struct {
	id           string
	baseURL      string
	registryHost string
	token        string
	client       *http.Client
}

func New(id, baseURL, registryHost, token string) *Provider {
	return &Provider{
		id:           id,
		baseURL:      strings.TrimRight(baseURL, "/"),
		registryHost: strings.TrimRight(registryHost, "/"),
		token:        token,
		client:       &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) ID() string { return p.id }

// GetImageURL renvoie l'image racine du projet (sans tag). Le registry
// n'accepte que des chemins en minuscules.
func (p *Provider) GetImageURL(repoPath string) string {
	return p.registryHost + "/" + strings.ToLower(repoPath)
}

// Auth : le registry GitLab accepte un personal access token comme mot de
// passe, quel que soit le username
func (p *Provider) Auth() *provider.RegistryAuth {
	if p.token == "" {
		return nil
	}
	return &provider.RegistryAuth{Username: "symphony", Password: p.token, ServerAddress: p.registryHost}
}

// ListTags liste les tags de l'image racine du projet, "latest" en tête.
// Un projet sans image publiée renvoie une liste vide ; ErrNotFound si le
// projet n'existe pas.
func (p *Provider) ListTags(repoPath string) ([]string, error) {
	req, err := http.NewRequest("GET",
		fmt.Sprintf("%s/api/v4/projects/%s/registry/repositories?tags=true", p.baseURL, url.PathEscape(repoPath)), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("PRIVATE-TOKEN", p.token)
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	if resp.StatusCode == http.StatusNotFound {
		return nil, provider.ErrNotFound
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("listTags: status %d — %s", resp.StatusCode, string(data))
	}

	var repos []struct {
		Path string `json:"path"`
		Tags []struct {
			Name string `json:"name"`
		} `json:"tags"`
	}
	if err := json.Unmarshal(data, &repos); err != nil {
		return nil, fmt.Errorf("listTags: %w", err)
	}

	tags := []string{}
	for _, r := range repos {
		if !strings.EqualFold(r.Path, repoPath) {
			continue // sous-image (<projet>/<nom>), pas celle déployée
		}
		for _, t := range r.Tags {
			tags = append(tags, t.Name)
		}
	}
	sort.SliceStable(tags, func(i, j int) bool { return tags[i] == "latest" && tags[j] != "latest" })
	return tags, nil
}

// DeleteImages : sans effet, GitLab purge le registry d'un projet avec lui
// (DeleteRepo attend la fin de cette purge)
func (p *Provider) DeleteImages(repoPath string) error { return nil }
