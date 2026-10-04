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

	"github.com/yourorg/symphony/internal/provider"
)

type Provider struct {
	id      string
	baseURL string
	token   string
	client  *http.Client
}

func New(id, baseURL, token string) *Provider {
	return &Provider{
		id:      id,
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client:  &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) ID() string { return p.id }

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.baseURL+"/api/v4"+path, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("PRIVATE-TOKEN", p.token)
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("gitlab api %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (p *Provider) CreateRepo(req provider.CreateRepoRequest) (*provider.Repo, error) {
	payload := map[string]any{
		"name":                   req.Name,
		"description":            req.Description,
		"initialize_with_readme": false,
		"visibility":             "private",
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
		return nil, fmt.Errorf("gitlab createRepo: status %d — %s", status, string(data))
	}

	var proj struct {
		ID                int    `json:"id"`
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURLToRepo     string `json:"http_url_to_repo"`
		SSHURLToRepo      string `json:"ssh_url_to_repo"`
		WebURL            string `json:"web_url"`
		DefaultBranch     string `json:"default_branch"`
	}
	json.Unmarshal(data, &proj)

	return &provider.Repo{
		ID:            fmt.Sprintf("%d", proj.ID),
		Name:          req.Name,
		Path:          proj.PathWithNamespace,
		WebURL:        proj.WebURL,
		HTTPCloneURL:  proj.HTTPURLToRepo,
		SSHCloneURL:   proj.SSHURLToRepo,
		DefaultBranch: "main",
	}, nil
}

func (p *Provider) PushFile(repoPath, branch, filePath, content, commitMsg string) error {
	encoded := url.PathEscape(repoPath)
	fileEncoded := url.PathEscape(filePath)

	payload := map[string]any{
		"branch":         branch,
		"content":        content,
		"commit_message": commitMsg,
	}

	_, status, err := p.api("POST",
		fmt.Sprintf("/projects/%s/repository/files/%s", encoded, fileEncoded),
		payload)
	if err != nil {
		return err
	}
	if status == 400 {
		// Fichier existe déjà — on met à jour
		_, status, err = p.api("PUT",
			fmt.Sprintf("/projects/%s/repository/files/%s", encoded, fileEncoded),
			payload)
		if err != nil {
			return err
		}
		if status != 200 {
			return fmt.Errorf("pushFile PUT %s: status %d", filePath, status)
		}
	} else if status != 201 {
		return fmt.Errorf("pushFile POST %s: status %d", filePath, status)
	}
	return nil
}

func (p *Provider) PushFiles(repoPath, branch string, files map[string]string, commitMsg string) error {
	// Utilise l'API commits pour pousser plusieurs fichiers en un seul commit
	encoded := url.PathEscape(repoPath)

	actions := make([]map[string]string, 0, len(files))
	for path, content := range files {
		actions = append(actions, map[string]string{
			"action":   "create",
			"file_path": path,
			"content":  content,
		})
	}

	payload := map[string]any{
		"branch":         branch,
		"commit_message": commitMsg,
		"actions":        actions,
	}

	_, status, err := p.api("POST",
		fmt.Sprintf("/projects/%s/repository/commits", encoded),
		payload)
	if err != nil {
		return err
	}
	if status != 201 {
		// Fallback: pousser fichier par fichier
		for path, content := range files {
			if err := p.PushFile(repoPath, branch, path, content, commitMsg); err != nil {
				return fmt.Errorf("pushFile %s: %w", path, err)
			}
		}
	}
	return nil
}

func (p *Provider) ListRepos() ([]provider.Repo, error) {
	data, _, err := p.api("GET", "/projects?membership=true&per_page=100&order_by=last_activity_at", nil)
	if err != nil {
		return nil, err
	}
	var projects []struct {
		ID                int    `json:"id"`
		Name              string `json:"name"`
		PathWithNamespace string `json:"path_with_namespace"`
		HTTPURLToRepo     string `json:"http_url_to_repo"`
		WebURL            string `json:"web_url"`
		DefaultBranch     string `json:"default_branch"`
	}
	json.Unmarshal(data, &projects)

	repos := make([]provider.Repo, len(projects))
	for i, proj := range projects {
		repos[i] = provider.Repo{
			ID:            fmt.Sprintf("%d", proj.ID),
			Name:          proj.Name,
			Path:          proj.PathWithNamespace,
			WebURL:        proj.WebURL,
			HTTPCloneURL:  proj.HTTPURLToRepo,
			DefaultBranch: proj.DefaultBranch,
		}
	}
	return repos, nil
}

func (p *Provider) GetFileContent(repoPath, branch, filePath string) (string, error) {
	encoded := url.PathEscape(repoPath)
	fileEncoded := url.PathEscape(filePath)
	data, status, err := p.api("GET",
		fmt.Sprintf("/projects/%s/repository/files/%s/raw?ref=%s", encoded, fileEncoded, branch),
		nil)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("getFile %s: status %d", filePath, status)
	}
	return string(data), nil
}

func (p *Provider) ListFiles(repoPath, branch, dirPath string) ([]string, error) {
	encoded := url.PathEscape(repoPath)
	apiPath := fmt.Sprintf("/projects/%s/repository/tree?path=%s&ref=%s&per_page=100&recursive=false",
		encoded, url.QueryEscape(dirPath), branch)

	data, _, err := p.api("GET", apiPath, nil)
	if err != nil {
		return nil, err
	}

	var items []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	json.Unmarshal(data, &items)

	var paths []string
	for _, item := range items {
		if item.Type == "blob" {
			paths = append(paths, item.Path)
		}
	}
	return paths, nil
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
	return 0, fmt.Errorf("namespace %q introuvable", name)
}
