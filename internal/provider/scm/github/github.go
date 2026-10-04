package github

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/yourorg/symphony/internal/provider"
)

type Provider struct {
	id     string
	token  string
	org    string
	client *http.Client
}

func New(id, token, org string) *Provider {
	return &Provider{
		id:     id,
		token:  token,
		org:    org,
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

func (p *Provider) ID() string { return p.id }

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, "https://api.github.com"+path, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+p.token)
	req.Header.Set("Accept", "application/vnd.github.v3+json")
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("github api: %w", err)
	}
	defer resp.Body.Close()
	data, _ := io.ReadAll(resp.Body)
	return data, resp.StatusCode, nil
}

func (p *Provider) CreateRepo(req provider.CreateRepoRequest) (*provider.Repo, error) {
	owner := p.org
	if req.Namespace != "" {
		owner = req.Namespace
	}

	payload := map[string]any{
		"name":        req.Name,
		"description": req.Description,
		"private":     req.Private,
		"auto_init":   false,
	}

	var apiPath string
	if p.org != "" {
		apiPath = "/orgs/" + owner + "/repos"
	} else {
		apiPath = "/user/repos"
	}

	data, status, err := p.api("POST", apiPath, payload)
	if err != nil {
		return nil, err
	}
	if status != 201 {
		return nil, fmt.Errorf("github createRepo: status %d — %s", status, string(data))
	}

	var repo struct {
		FullName      string `json:"full_name"`
		Name          string `json:"name"`
		HTMLURL       string `json:"html_url"`
		CloneURL      string `json:"clone_url"`
		SSHURL        string `json:"ssh_url"`
		DefaultBranch string `json:"default_branch"`
	}
	json.Unmarshal(data, &repo)

	return &provider.Repo{
		ID:            repo.FullName,
		Name:          repo.Name,
		Path:          repo.FullName,
		WebURL:        repo.HTMLURL,
		HTTPCloneURL:  repo.CloneURL,
		SSHCloneURL:   repo.SSHURL,
		DefaultBranch: "main",
	}, nil
}

func (p *Provider) PushFile(repoPath, branch, filePath, content, commitMsg string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	// Vérifier si le fichier existe pour récupérer le SHA
	var sha string
	existing, status, _ := p.api("GET",
		fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repoPath, filePath, branch), nil)
	if status == 200 {
		var f struct{ SHA string `json:"sha"` }
		json.Unmarshal(existing, &f)
		sha = f.SHA
	}

	payload := map[string]any{
		"message": commitMsg,
		"content": encoded,
		"branch":  branch,
	}
	if sha != "" {
		payload["sha"] = sha
	}

	_, status, err := p.api("PUT",
		fmt.Sprintf("/repos/%s/contents/%s", repoPath, filePath), payload)
	if err != nil {
		return err
	}
	if status != 200 && status != 201 {
		return fmt.Errorf("github pushFile %s: status %d", filePath, status)
	}
	return nil
}

func (p *Provider) PushFiles(repoPath, branch string, files map[string]string, commitMsg string) error {
	// GitHub n'a pas d'API multi-fichiers simple — on pousse fichier par fichier
	for path, content := range files {
		if err := p.PushFile(repoPath, branch, path, content, commitMsg); err != nil {
			return fmt.Errorf("pushFile %s: %w", path, err)
		}
	}
	return nil
}

func (p *Provider) ListRepos() ([]provider.Repo, error) {
	var apiPath string
	if p.org != "" {
		apiPath = "/orgs/" + p.org + "/repos?per_page=100&type=all"
	} else {
		apiPath = "/user/repos?per_page=100&type=all"
	}

	data, status, err := p.api("GET", apiPath, nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("listRepos: status %d — %s", status, string(data))
	}

	var repos []struct {
		FullName      string `json:"full_name"`
		Name          string `json:"name"`
		HTMLURL       string `json:"html_url"`
		CloneURL      string `json:"clone_url"`
		DefaultBranch string `json:"default_branch"`
	}
	json.Unmarshal(data, &repos)

	out := make([]provider.Repo, len(repos))
	for i, r := range repos {
		out[i] = provider.Repo{
			ID:            r.FullName,
			Name:          r.Name,
			Path:          r.FullName,
			WebURL:        r.HTMLURL,
			HTTPCloneURL:  r.CloneURL,
			DefaultBranch: r.DefaultBranch,
		}
	}
	return out, nil
}

func (p *Provider) GetFileContent(repoPath, branch, filePath string) (string, error) {
	data, status, err := p.api("GET",
		fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repoPath, filePath, branch), nil)
	if err != nil {
		return "", err
	}
	if status != 200 {
		return "", fmt.Errorf("getFile %s: status %d", filePath, status)
	}

	var f struct {
		Content  string `json:"content"`
		Encoding string `json:"encoding"`
	}
	json.Unmarshal(data, &f)

	if f.Encoding == "base64" {
		decoded, err := base64.StdEncoding.DecodeString(
			strings.ReplaceAll(f.Content, "\n", ""))
		if err != nil {
			return "", err
		}
		return string(decoded), nil
	}
	return f.Content, nil
}

func (p *Provider) ListFiles(repoPath, branch, dirPath string) ([]string, error) {
	data, status, err := p.api("GET",
		fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repoPath, dirPath, branch), nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("listFiles %s: status %d", dirPath, status)
	}

	var items []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	json.Unmarshal(data, &items)

	var paths []string
	for _, item := range items {
		if item.Type == "file" {
			paths = append(paths, item.Path)
		}
	}
	return paths, nil
}
