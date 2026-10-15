package github

import (
	"bytes"
	"cmp"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

type Provider struct {
	id     string
	token  string
	org    string
	apiURL string
	client *http.Client
}

func New(id, token, org string) *Provider {
	return &Provider{
		id:     id,
		token:  token,
		org:    org,
		apiURL: "https://api.github.com",
		client: &http.Client{Timeout: 15 * time.Second},
	}
}

// WithAPIURL : autre API GitHub (tests, GitHub Enterprise)
func (p *Provider) WithAPIURL(u string) *Provider {
	p.apiURL = strings.TrimRight(u, "/")
	return p
}

func (p *Provider) ID() string { return p.id }

func (p *Provider) Platform() string { return "github" }

func (p *Provider) api(method, path string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, _ := json.Marshal(body)
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.apiURL+path, buf)
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

// CreateRepo crée le repo dans l'organisation (Namespace, sinon org), ou
// dans le compte du jeton. Il est initialisé (README) : l'API Git, qui
// pousse le squelette en un commit, ne travaille pas sur un repo vide.
func (p *Provider) CreateRepo(req provider.CreateRepoRequest) (*provider.Repo, error) {
	owner := p.org
	if req.Namespace != "" {
		owner = req.Namespace
	}

	payload := map[string]any{
		"name":        req.Name,
		"description": req.Description,
		"private":     req.Private,
		"auto_init":   true,
	}

	apiPath := "/user/repos"
	if owner != "" {
		apiPath = "/orgs/" + owner + "/repos"
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
		DefaultBranch: cmp.Or(repo.DefaultBranch, "main"),
	}, nil
}

func (p *Provider) PushFile(repoPath, branch, filePath, content, commitMsg string) error {
	encoded := base64.StdEncoding.EncodeToString([]byte(content))

	// Vérifier si le fichier existe pour récupérer le SHA
	var sha string
	existing, status, _ := p.api("GET",
		fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repoPath, filePath, branch), nil)
	if status == 200 {
		var f struct {
			SHA string `json:"sha"`
		}
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

// PushFiles pousse tous les fichiers en un seul commit (API Git : arbre,
// commit, mise à jour de la branche). Un commit par fichier lancerait un
// workflow par fichier.
func (p *Provider) PushFiles(repoPath, branch string, files map[string]string, commitMsg string) error {
	var ref struct {
		Object struct {
			SHA string `json:"sha"`
		} `json:"object"`
	}
	// Juste après CreateRepo, le commit initial peut ne pas être encore visible
	var err error
	for attempt := 0; attempt < 5; attempt++ {
		if err = p.git("GET", repoPath, "/git/ref/heads/"+branch, nil, http.StatusOK, &ref); err == nil {
			break
		}
		time.Sleep(time.Duration(attempt+1) * time.Second)
	}
	if err != nil {
		return fmt.Errorf("branche %s: %w", branch, err)
	}
	var parent struct {
		Tree struct {
			SHA string `json:"sha"`
		} `json:"tree"`
	}
	if err := p.git("GET", repoPath, "/git/commits/"+ref.Object.SHA, nil, http.StatusOK, &parent); err != nil {
		return err
	}

	paths := make([]string, 0, len(files))
	for path := range files {
		paths = append(paths, path)
	}
	sort.Strings(paths)
	entries := make([]map[string]string, len(paths))
	for i, path := range paths {
		entries[i] = map[string]string{"path": path, "mode": "100644", "type": "blob", "content": files[path]}
	}
	var tree, commit struct {
		SHA string `json:"sha"`
	}
	if err := p.git("POST", repoPath, "/git/trees", map[string]any{
		"base_tree": parent.Tree.SHA, "tree": entries,
	}, http.StatusCreated, &tree); err != nil {
		return err
	}
	if err := p.git("POST", repoPath, "/git/commits", map[string]any{
		"message": commitMsg, "tree": tree.SHA, "parents": []string{ref.Object.SHA},
	}, http.StatusCreated, &commit); err != nil {
		return err
	}
	return p.git("PATCH", repoPath, "/git/refs/heads/"+branch, map[string]any{"sha": commit.SHA}, http.StatusOK, nil)
}

// git appelle l'API Git du repo et décode la réponse dans v si le statut est want
func (p *Provider) git(method, repoPath, path string, body any, want int, v any) error {
	data, status, err := p.api(method, "/repos/"+repoPath+path, body)
	if err != nil {
		return err
	}
	if status == http.StatusNotFound {
		return fmt.Errorf("github %s %s: %w", method, path, provider.ErrNotFound)
	}
	if status != want {
		return fmt.Errorf("github %s %s: status %d — %s", method, path, status, strings.TrimSpace(string(data)))
	}
	if v != nil {
		return json.Unmarshal(data, v)
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
	if status == 404 {
		return "", fmt.Errorf("getFile %s: %w", filePath, provider.ErrNotFound)
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
	if status == 404 {
		return nil, fmt.Errorf("listFiles %s: %w", dirPath, provider.ErrNotFound)
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

	// L'API contents n'est pas récursive : on descend dans les sous-dossiers
	var paths []string
	for _, item := range items {
		switch item.Type {
		case "file":
			paths = append(paths, item.Path)
		case "dir":
			sub, err := p.ListFiles(repoPath, branch, item.Path)
			if err != nil {
				return nil, err
			}
			paths = append(paths, sub...)
		}
	}
	return paths, nil
}

func (p *Provider) DeleteFile(repoPath, branch, filePath, commitMsg string) error {
	// L'API contents exige le SHA du fichier à supprimer
	existing, status, err := p.api("GET",
		fmt.Sprintf("/repos/%s/contents/%s?ref=%s", repoPath, filePath, branch), nil)
	if err != nil {
		return err
	}
	if status == 404 {
		return provider.ErrNotFound
	}
	if status != 200 {
		return fmt.Errorf("github deleteFile %s: status %d", filePath, status)
	}
	var f struct {
		SHA string `json:"sha"`
	}
	json.Unmarshal(existing, &f)

	_, status, err = p.api("DELETE", fmt.Sprintf("/repos/%s/contents/%s", repoPath, filePath),
		map[string]any{"message": commitMsg, "sha": f.SHA, "branch": branch})
	if err != nil {
		return err
	}
	if status != 200 {
		return fmt.Errorf("github deleteFile %s: status %d", filePath, status)
	}
	return nil
}

// CreateWebhook : Symphony ne reçoit pas encore les webhooks de GitHub ; les
// tâches suivent les workflows par sondage
func (p *Provider) CreateWebhook(repoPath, url, secret string) error {
	return provider.ErrUnsupported
}

// DeleteRepo nécessite un token avec le scope delete_repo
func (p *Provider) DeleteRepo(repoPath string) error {
	data, status, err := p.api("DELETE", "/repos/"+repoPath, nil)
	if err != nil {
		return err
	}
	switch status {
	case 204:
		return nil
	case 404:
		return provider.ErrNotFound
	default:
		return fmt.Errorf("github deleteRepo %s: status %d — %s", repoPath, status, string(data))
	}
}
