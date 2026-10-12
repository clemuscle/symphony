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

	"github.com/clemuscle/symphony/internal/provider"
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

func (p *Provider) Platform() string { return "gitlab" }

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

// PushFiles pousse les fichiers en un seul commit (API commits). Chaque
// fichier est d'abord créé ; si certains existent déjà (GitLab répond 400), le
// commit est refait en les mettant à jour : un redéploiement reste un commit.
func (p *Provider) PushFiles(repoPath, branch string, files map[string]string, commitMsg string) error {
	data, status, err := p.commitFiles(repoPath, branch, files, commitMsg, nil)
	if err != nil {
		return err
	}
	if status == 400 {
		existing := make(map[string]bool, len(files))
		for path := range files {
			ok, err := p.fileExists(repoPath, branch, path)
			if err != nil {
				return err
			}
			existing[path] = ok
		}
		data, status, err = p.commitFiles(repoPath, branch, files, commitMsg, existing)
		if err != nil {
			return err
		}
	}
	if status != 201 {
		return fmt.Errorf("pushFiles: status %d — %s", status, string(data))
	}
	return nil
}

// commitFiles crée un commit : action update pour les fichiers de existing, create pour les autres
func (p *Provider) commitFiles(repoPath, branch string, files map[string]string, commitMsg string, existing map[string]bool) ([]byte, int, error) {
	actions := make([]map[string]string, 0, len(files))
	for path, content := range files {
		action := "create"
		if existing[path] {
			action = "update"
		}
		actions = append(actions, map[string]string{
			"action":    action,
			"file_path": path,
			"content":   content,
		})
	}
	return p.api("POST", fmt.Sprintf("/projects/%s/repository/commits", url.PathEscape(repoPath)), map[string]any{
		"branch":         branch,
		"commit_message": commitMsg,
		"actions":        actions,
	})
}

func (p *Provider) fileExists(repoPath, branch, filePath string) (bool, error) {
	_, status, err := p.api("HEAD", fmt.Sprintf("/projects/%s/repository/files/%s?ref=%s",
		url.PathEscape(repoPath), url.PathEscape(filePath), url.QueryEscape(branch)), nil)
	if err != nil {
		return false, err
	}
	switch status {
	case 200:
		return true, nil
	case 404:
		return false, nil
	}
	return false, fmt.Errorf("fichier %s: status %d", filePath, status)
}

func (p *Provider) ListRepos() ([]provider.Repo, error) {
	data, status, err := p.api("GET", "/projects?membership=true&per_page=100&order_by=last_activity_at", nil)
	if err != nil {
		return nil, err
	}
	if status != 200 {
		return nil, fmt.Errorf("listRepos: status %d — %s", status, string(data))
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
	if status == 404 {
		return "", fmt.Errorf("getFile %s: %w", filePath, provider.ErrNotFound)
	}
	if status != 200 {
		return "", fmt.Errorf("getFile %s: status %d", filePath, status)
	}
	return string(data), nil
}

func (p *Provider) ListFiles(repoPath, branch, dirPath string) ([]string, error) {
	const perPage = 100
	encoded := url.PathEscape(repoPath)

	// Pagination : sans elle, un arbre de plus de 100 entrées serait tronqué
	var paths []string
	for page := 1; ; page++ {
		apiPath := fmt.Sprintf("/projects/%s/repository/tree?path=%s&ref=%s&per_page=%d&page=%d&recursive=true",
			encoded, url.QueryEscape(dirPath), url.QueryEscape(branch), perPage, page)

		data, status, err := p.api("GET", apiPath, nil)
		if err != nil {
			return nil, err
		}
		if status != 200 {
			return nil, fmt.Errorf("listFiles %s: status %d — %s", dirPath, status, string(data))
		}

		var items []struct {
			Name string `json:"name"`
			Path string `json:"path"`
			Type string `json:"type"`
		}
		if err := json.Unmarshal(data, &items); err != nil {
			return nil, fmt.Errorf("listFiles %s: %w", dirPath, err)
		}

		for _, item := range items {
			if item.Type == "blob" {
				paths = append(paths, item.Path)
			}
		}
		if len(items) < perPage {
			return paths, nil
		}
	}
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

func (p *Provider) DeleteFile(repoPath, branch, filePath, commitMsg string) error {
	_, status, err := p.api("DELETE",
		fmt.Sprintf("/projects/%s/repository/files/%s", url.PathEscape(repoPath), url.PathEscape(filePath)),
		map[string]any{"branch": branch, "commit_message": commitMsg})
	if err != nil {
		return err
	}
	switch status {
	case 204:
		return nil
	case 404:
		return provider.ErrNotFound
	default:
		return fmt.Errorf("deleteFile %s: status %d", filePath, status)
	}
}

// DeleteRepo supprime le projet. GitLab refuse tant que le container registry
// contient des tags : on les supprime d'abord, puis on réessaie le temps que
// GitLab termine la purge (asynchrone, souvent 2 à 3 min).
func (p *Provider) DeleteRepo(repoPath string) error {
	encoded := url.PathEscape(repoPath)

	data, status, err := p.api("GET", "/projects/"+encoded+"/registry/repositories", nil)
	if err != nil {
		return err
	}
	if status == 404 {
		return provider.ErrNotFound
	}
	if status == 200 {
		var repos []struct {
			ID int `json:"id"`
		}
		json.Unmarshal(data, &repos)
		for _, r := range repos {
			if _, st, err := p.api("DELETE",
				fmt.Sprintf("/projects/%s/registry/repositories/%d", encoded, r.ID), nil); err != nil || st != 202 {
				return fmt.Errorf("deleteRepo: purge registry %d: status %d %v", r.ID, st, err)
			}
		}
	}

	deadline := time.Now().Add(10 * time.Minute)
	for {
		data, status, err := p.api("DELETE", "/projects/"+encoded, nil)
		if err != nil {
			return err
		}
		switch {
		case status == 202 || status == 204:
			return nil
		case status == 404:
			return provider.ErrNotFound
		case status == 400 && strings.Contains(string(data), "registry") && time.Now().Before(deadline):
			time.Sleep(5 * time.Second)
		default:
			return fmt.Errorf("deleteRepo %s: status %d — %s", repoPath, status, string(data))
		}
	}
}
