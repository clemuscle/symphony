package gitlab

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
	BaseURL string
	Token   string
	client  *http.Client
}

func New(baseURL, token string) *Provider {
	return &Provider{
		BaseURL: strings.TrimRight(baseURL, "/"),
		Token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, _ := http.NewRequest(method, p.BaseURL+"/api/v4"+path, buf)
	req.Header.Set("PRIVATE-TOKEN", p.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (p *Provider) CreateRepo(req providers.RepoRequest) (*providers.RepoResult, error) {
	payload := map[string]any{
		"name":                    req.Name,
		"description":             req.Description,
		"initialize_with_readme":  true,
		"visibility":              "private",
	}
	if req.Namespace != "" {
		if nsID, err := p.resolveNamespace(req.Namespace); err == nil {
			payload["namespace_id"] = nsID
		}
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
	json.Unmarshal(data, &project)

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
	data, _, err := p.api("GET", "/projects?membership=true&per_page=50", nil)
	if err != nil {
		return nil, err
	}
	var projects []struct {
		Name   string `json:"name"`
		Path   string `json:"path_with_namespace"`
		WebURL string `json:"web_url"`
	}
	json.Unmarshal(data, &projects)

	repos := make([]providers.Repo, len(projects))
	for i, p := range projects {
		repos[i] = providers.Repo{Name: p.Name, Path: p.Path, WebURL: p.WebURL}
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
	json.Unmarshal(data, &ns)
	for _, n := range ns {
		if n.Path == name {
			return n.ID, nil
		}
	}
	return 0, fmt.Errorf("namespace %s not found", name)
}
