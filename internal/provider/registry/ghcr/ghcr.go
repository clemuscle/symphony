// Package ghcr : GitHub Container Registry. Les images d'un repo
// owner/name sont publiées par son workflow sous ghcr.io/owner/name (en
// minuscules) ; tags et suppression passent par l'API des packages GitHub.
// Jeton : read:packages (tags, pull) et delete:packages (suppression).
package ghcr

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

const host = "ghcr.io"

type Provider struct {
	id      string
	owner   string // propriétaire par défaut des images (si repoPath n'en a pas)
	token   string
	apiURL  string
	client  *http.Client
	mu      sync.Mutex
	login   string            // utilisateur du jeton, lu au premier besoin
	ownerOf map[string]string // owner → "orgs/<owner>" ou "user"
}

func New(id, owner, token string) *Provider {
	return &Provider{
		id:      id,
		owner:   owner,
		token:   token,
		apiURL:  "https://api.github.com",
		client:  &http.Client{Timeout: 15 * time.Second},
		ownerOf: map[string]string{},
	}
}

// WithAPIURL : autre API GitHub (tests, GitHub Enterprise)
func (p *Provider) WithAPIURL(u string) *Provider {
	p.apiURL = strings.TrimRight(u, "/")
	return p
}

func (p *Provider) ID() string { return p.id }

// split : propriétaire et nom du package d'un repo GitHub owner/name
func (p *Provider) split(repoPath string) (owner, name string) {
	repoPath = strings.ToLower(strings.Trim(repoPath, "/"))
	if o, n, ok := strings.Cut(repoPath, "/"); ok {
		return o, n
	}
	return strings.ToLower(p.owner), repoPath
}

func (p *Provider) GetImageURL(repoPath string) string {
	owner, name := p.split(repoPath)
	return host + "/" + owner + "/" + name
}

// Auth : GHCR accepte un jeton GitHub comme mot de passe, avec son utilisateur
func (p *Provider) Auth() *provider.RegistryAuth {
	if p.token == "" {
		return nil
	}
	login, err := p.user()
	if err != nil {
		login = "symphony" // le pull dira si le jeton est refusé
	}
	return &provider.RegistryAuth{Username: login, Password: p.token, ServerAddress: host}
}

// ListTags : tags des versions du package, la plus récente d'abord ("latest"
// en tête). Un repo sans image publiée renvoie une liste vide.
func (p *Provider) ListTags(repoPath string) ([]string, error) {
	owner, name := p.split(repoPath)
	base, err := p.packages(owner)
	if err != nil {
		return nil, err
	}
	var versions []struct {
		UpdatedAt time.Time `json:"updated_at"`
		Metadata  struct {
			Container struct {
				Tags []string `json:"tags"`
			} `json:"container"`
		} `json:"metadata"`
	}
	status, err := p.api("GET", base+"/packages/container/"+url.PathEscape(name)+"/versions?per_page=100", &versions)
	if err != nil {
		return nil, err
	}
	if status == http.StatusNotFound {
		return []string{}, nil
	}
	sort.SliceStable(versions, func(i, j int) bool { return versions[i].UpdatedAt.After(versions[j].UpdatedAt) })
	tags := []string{}
	hasLatest := false
	for _, v := range versions {
		for _, t := range v.Metadata.Container.Tags {
			if t == "latest" {
				hasLatest = true
				continue
			}
			tags = append(tags, t)
		}
	}
	if hasLatest {
		tags = append([]string{"latest"}, tags...)
	}
	return tags, nil
}

// DeleteImages supprime le package (toutes ses versions) : supprimer le repo
// GitHub ne le fait pas
func (p *Provider) DeleteImages(repoPath string) error {
	owner, name := p.split(repoPath)
	base, err := p.packages(owner)
	if err != nil {
		return err
	}
	status, err := p.api("DELETE", base+"/packages/container/"+url.PathEscape(name), nil)
	if err != nil {
		return err
	}
	switch status {
	case http.StatusNoContent, http.StatusNotFound:
		return nil
	}
	return fmt.Errorf("ghcr: suppression du package %s/%s : status %d", owner, name, status)
}

// packages : préfixe de l'API des packages pour owner : /orgs/<owner> pour
// une organisation, /user pour l'utilisateur du jeton
func (p *Provider) packages(owner string) (string, error) {
	p.mu.Lock()
	base, ok := p.ownerOf[owner]
	p.mu.Unlock()
	if ok {
		return base, nil
	}
	login, err := p.user()
	if err != nil {
		return "", err
	}
	base = "/orgs/" + url.PathEscape(owner)
	if strings.EqualFold(login, owner) {
		base = "/user"
	}
	p.mu.Lock()
	p.ownerOf[owner] = base
	p.mu.Unlock()
	return base, nil
}

// user : login de l'utilisateur du jeton
func (p *Provider) user() (string, error) {
	p.mu.Lock()
	login := p.login
	p.mu.Unlock()
	if login != "" {
		return login, nil
	}
	var u struct {
		Login string `json:"login"`
	}
	status, err := p.api("GET", "/user", &u)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK || u.Login == "" {
		return "", fmt.Errorf("ghcr: utilisateur du jeton illisible (status %d)", status)
	}
	p.mu.Lock()
	p.login = u.Login
	p.mu.Unlock()
	return u.Login, nil
}

// api appelle l'API GitHub ; v reçoit la réponse si elle est en 2xx. Les
// statuts autres que 2xx et 404 sont des erreurs.
func (p *Provider) api(method, path string, v any) (int, error) {
	req, err := http.NewRequest(method, p.apiURL+path, nil)
	if err != nil {
		return 0, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	resp, err := p.client.Do(req)
	if err != nil {
		return 0, fmt.Errorf("ghcr: %w", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	switch {
	case resp.StatusCode == http.StatusNotFound:
		return resp.StatusCode, nil
	case resp.StatusCode >= 300:
		return resp.StatusCode, fmt.Errorf("ghcr: %s %s : status %d — %s", method, path, resp.StatusCode, strings.TrimSpace(string(body)))
	}
	if v != nil && len(body) > 0 {
		if err := json.Unmarshal(body, v); err != nil {
			return resp.StatusCode, fmt.Errorf("ghcr: réponse illisible: %w", err)
		}
	}
	return resp.StatusCode, nil
}

// Check : l'utilisateur du jeton (GET /user)
func (p *Provider) Check() (string, error) {
	var u struct {
		Login string `json:"login"`
	}
	status, err := p.api("GET", "/user", &u)
	if err != nil {
		return "", err
	}
	if status == http.StatusNotFound || u.Login == "" {
		return "", fmt.Errorf("ghcr: utilisateur du jeton illisible (status %d)", status)
	}
	return "connecté en tant que " + u.Login, nil
}
