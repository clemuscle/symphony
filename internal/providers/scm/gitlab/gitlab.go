package gitlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/yourorg/symphony/internal/providers"
)

type Provider struct {
	BaseURL string
	Token   string
	client  *http.Client
}

func New(baseURL, token string) (*Provider, error) {
	if baseURL == "" {
		return nil, fmt.Errorf("gitlab: baseURL is required")
	}
	if token == "" {
		return nil, fmt.Errorf("gitlab: token is required")
	}
	return &Provider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}, nil
}

// Ping vérifie la connectivité et la validité du token via GET /api/v4/user.
func (p *Provider) Ping() error {
	_, status, err := p.api("GET", "/user", nil)
	if err != nil {
		return fmt.Errorf("gitlab ping: %w", err)
	}
	if status == 401 {
		return fmt.Errorf("gitlab ping: token invalide ou expiré (401)")
	}
	if status != 200 {
		return fmt.Errorf("gitlab ping: status inattendu %d", status)
	}
	return nil
}

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	data, status, _, err := p.apiWithHeaders(method, path, body)
	return data, status, err
}

func (p *Provider) apiWithHeaders(method, path string, body any) ([]byte, int, http.Header, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, nil, fmt.Errorf("gitlab %s %s: marshal request: %w", method, path, err)
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.BaseURL+"/api/v4"+path, buf)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("gitlab %s %s: build request: %w", method, path, err)
	}
	req.Header.Set("PRIVATE-TOKEN", p.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("gitlab %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, 0, nil, fmt.Errorf("gitlab %s %s: read response: %w", method, path, err)
	}
	return data, resp.StatusCode, resp.Header, nil
}

func (p *Provider) CreateRepo(req providers.RepoRequest) (*providers.RepoResult, error) {
	payload := map[string]any{
		"name":                    req.Name,
		"description":             req.Description,
		"initialize_with_readme":  true,
		"visibility":              "private",
	}
	if req.Namespace != "" {
		nsID, err := p.resolveNamespace(req.Namespace)
		if err != nil {
			return nil, fmt.Errorf("gitlab createRepo: resolve namespace %q: %w", req.Namespace, err)
		}
		payload["namespace_id"] = nsID
	}

	data, status, err := p.api("POST", "/projects", payload)
	if err != nil {
		return nil, err
	}
	if status != 201 {
		return nil, fmt.Errorf("gitlab createRepo: %d — %s", status, string(data))
	}

	var project struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURLToRepo     string `json:"http_url_to_repo"`
		SSHURLToRepo      string `json:"ssh_url_to_repo"`
		WebURL            string `json:"web_url"`
		DefaultBranch     string `json:"default_branch"`
	}
	if err := json.Unmarshal(data, &project); err != nil {
		return nil, fmt.Errorf("gitlab createRepo: decode response: %w", err)
	}

	return &providers.RepoResult{
		ID:            project.ID,
		Path:          project.PathWithNamespace,
		WebURL:        project.WebURL,
		HTTPCloneURL:  project.HTTPURLToRepo,
		SSHCloneURL:   project.SSHURLToRepo,
		DefaultBranch: project.DefaultBranch,
	}, nil
}

func (p *Provider) PushFile(projectPath, branch, filePath, content, commitMsg string) error {
	encoded := url.PathEscape(projectPath)
	payload := map[string]any{
		"branch":         branch,
		"content":        content,
		"commit_message": commitMsg,
	}
	_, status, err := p.api("POST",
		fmt.Sprintf("/projects/%s/repository/files/%s", encoded, url.PathEscape(filePath)),
		payload)
	if err != nil {
		return err
	}
	if status != 201 {
		return fmt.Errorf("pushFile %s: status %d", filePath, status)
	}
	return nil
}

func (p *Provider) ListRepos() ([]providers.Repo, error) {
	var repos []providers.Repo
	page := 1
	for {
		data, status, headers, err := p.apiWithHeaders("GET",
			fmt.Sprintf("/projects?membership=true&per_page=50&page=%d", page), nil)
		if err != nil {
			return nil, err
		}
		if status < 200 || status >= 300 {
			return nil, fmt.Errorf("gitlab listRepos: %d — %s", status, string(data))
		}

		var projects []struct {
			Name   string `json:"name"`
			Path   string `json:"path_with_namespace"`
			WebURL string `json:"web_url"`
		}
		if err := json.Unmarshal(data, &projects); err != nil {
			return nil, fmt.Errorf("gitlab listRepos: decode response: %w", err)
		}
		for _, proj := range projects {
			repos = append(repos, providers.Repo{Name: proj.Name, Path: proj.Path, WebURL: proj.WebURL})
		}

		nextPage := headers.Get("X-Next-Page")
		if nextPage == "" {
			break
		}
		page, err = strconv.Atoi(nextPage)
		if err != nil {
			break
		}
	}
	return repos, nil
}

func (p *Provider) resolveNamespace(name string) (int, error) {
	data, _, err := p.api("GET", "/namespaces?search="+url.QueryEscape(name), nil)
	if err != nil {
		return 0, err
	}
	var ns []struct {
		ID   int    `json:"id"`
		Path string `json:"path"`
	}
	if err := json.Unmarshal(data, &ns); err != nil {
		return 0, fmt.Errorf("gitlab resolveNamespace: decode response: %w", err)
	}
	for _, n := range ns {
		if n.Path == name {
			return n.ID, nil
		}
	}
	return 0, fmt.Errorf("namespace %s not found", name)
}
