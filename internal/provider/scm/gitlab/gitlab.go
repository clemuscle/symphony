package gitlab

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

type Provider struct {
	id      string
	baseURL string
	token   string
	client  *http.Client
	// Groupe des repos créés sans namespace explicite (vide : compte du token)
	namespace string

	// Un commit à la fois par repo : GitLab refuse (400) des commits
	// simultanés sur une même branche, ce qui arrive quand plusieurs tâches
	// écrivent dans le repo de config ou de déploiement en même temps
	writeMu sync.Mutex
	repoMu  map[string]*sync.Mutex
}

// lockRepo réserve les écritures dans repoPath ; renvoie la fonction qui libère
func (p *Provider) lockRepo(repoPath string) func() {
	p.writeMu.Lock()
	if p.repoMu == nil {
		p.repoMu = map[string]*sync.Mutex{}
	}
	mu, ok := p.repoMu[repoPath]
	if !ok {
		mu = &sync.Mutex{}
		p.repoMu[repoPath] = mu
	}
	p.writeMu.Unlock()
	mu.Lock()
	return mu.Unlock
}

func New(id, baseURL, token string) *Provider {
	return &Provider{
		id:      id,
		baseURL: strings.TrimRight(baseURL, "/"),
		token:   token,
		client: &http.Client{
			Timeout: 15 * time.Second,
			// L'API redirige l'ancien chemin d'un projet renommé (ou planifié
			// pour suppression) vers son external_url, qui peut être injoignable
			// d'ici (localhost vu d'un conteneur) : la redirection est rendue telle quelle
			CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
		},
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

// WithNamespace : groupe par défaut des repos créés (config.namespace)
func (p *Provider) WithNamespace(ns string) *Provider {
	p.namespace = strings.Trim(ns, "/")
	return p
}

func (p *Provider) CreateRepo(req provider.CreateRepoRequest) (*provider.Repo, error) {
	payload := map[string]any{
		"name":                   req.Name,
		"description":            req.Description,
		"initialize_with_readme": false,
		"visibility":             "private",
	}
	// Un groupe introuvable est une erreur : le repo ne doit pas atterrir
	// ailleurs, hors des droits prévus
	if ns := strings.Trim(req.Namespace, "/"); ns != "" || p.namespace != "" {
		if ns == "" {
			ns = p.namespace
		}
		nsID, err := p.resolveNamespace(ns)
		if err != nil {
			return nil, err
		}
		payload["namespace_id"] = nsID
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
	if err := json.Unmarshal(data, &proj); err != nil {
		return nil, fmt.Errorf("gitlab createRepo : réponse illisible: %w", err)
	}

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
	defer p.lockRepo(repoPath)()
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
	defer p.lockRepo(repoPath)()
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
	if err := json.Unmarshal(data, &projects); err != nil {
		return nil, fmt.Errorf("gitlab listRepos : réponse illisible: %w", err)
	}

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
		if status == 404 {
			return nil, fmt.Errorf("listFiles %s: %w", dirPath, provider.ErrNotFound)
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

// resolveNamespace : id d'un groupe (ou d'un utilisateur) par son chemin
// complet, sous-groupes compris (apps/team-a)
func (p *Provider) resolveNamespace(name string) (int, error) {
	data, status, err := p.api("GET", "/namespaces/"+url.PathEscape(name), nil)
	if err != nil {
		return 0, err
	}
	if status == 404 {
		return 0, fmt.Errorf("namespace %q introuvable: %w", name, provider.ErrNotFound)
	}
	if status != 200 {
		return 0, fmt.Errorf("namespace %q: status %d — %s", name, status, string(data))
	}
	var ns struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(data, &ns); err != nil {
		return 0, fmt.Errorf("namespace %q: %w", name, err)
	}
	return ns.ID, nil
}

func (p *Provider) DeleteFile(repoPath, branch, filePath, commitMsg string) error {
	defer p.lockRepo(repoPath)()
	data, status, err := p.api("DELETE",
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
	case 400:
		// Fichier absent : GitLab répond 400, pas 404
		if strings.Contains(string(data), "doesn't exist") {
			return provider.ErrNotFound
		}
	}
	return fmt.Errorf("deleteFile %s: status %d — %s", filePath, status, strings.TrimSpace(string(data)))
}

// removePermanently supprime pour de bon un projet que GitLab n'a fait que
// programmer pour suppression (suppression différée, par défaut depuis GitLab
// 18 : le projet est renommé en -deletion_scheduled-<id> et gardé plusieurs
// jours). La seconde demande doit nommer son nouveau chemin. id : l'id
// numérique, qui ne change pas avec le renommage.
func (p *Provider) removePermanently(id int) error {
	data, status, err := p.api("GET", fmt.Sprintf("/projects/%d", id), nil)
	if err != nil {
		return err
	}
	if status == 404 {
		return nil // pas de suppression différée : déjà supprimé
	}
	if status != 200 {
		return fmt.Errorf("deleteRepo: projet %d: status %d — %s", id, status, string(data))
	}
	var proj struct {
		PathWithNamespace   string `json:"path_with_namespace"`
		MarkedForDeletionOn string `json:"marked_for_deletion_on"`
	}
	if err := json.Unmarshal(data, &proj); err != nil {
		return fmt.Errorf("gitlab removePermanently : réponse illisible: %w", err)
	}
	if proj.MarkedForDeletionOn == "" {
		return nil
	}
	data, status, err = p.api("DELETE", fmt.Sprintf("/projects/%d?permanently_remove=true&full_path=%s",
		id, url.QueryEscape(proj.PathWithNamespace)), nil)
	if err != nil {
		return err
	}
	if status != 202 && status != 204 && status != 404 {
		return fmt.Errorf("suppression définitive de %s: status %d — %s", proj.PathWithNamespace, status, string(data))
	}
	return nil
}

// EnsureWebhook abonne url aux événements de pipeline du projet ; un webhook
// vers url qui existe déjà reçoit le secret courant
func (p *Provider) EnsureWebhook(repoPath, hookURL, secret string) error {
	hooks := "/projects/" + url.PathEscape(repoPath) + "/hooks"
	data, status, err := p.api("GET", hooks, nil)
	if err != nil {
		return err
	}
	if status == 404 {
		return provider.ErrNotFound
	}
	if status != 200 {
		return fmt.Errorf("liste des webhooks: status %d — %s", status, string(data))
	}
	var existing []struct {
		ID  int64  `json:"id"`
		URL string `json:"url"`
	}
	if err := json.Unmarshal(data, &existing); err != nil {
		return fmt.Errorf("gitlab createWebhook : réponse illisible: %w", err)
	}
	hook := map[string]any{
		"url":                     hookURL,
		"token":                   secret, // renvoyé dans l'en-tête X-Gitlab-Token
		"pipeline_events":         true,
		"push_events":             false,
		"enable_ssl_verification": strings.HasPrefix(hookURL, "https://"),
	}
	// Déjà abonné : GitLab ne rend pas le secret, on le réécrit (rotation)
	for _, h := range existing {
		if h.URL == hookURL {
			data, status, err = p.api("PUT", fmt.Sprintf("%s/%d", hooks, h.ID), hook)
			if err != nil {
				return err
			}
			if status != 200 {
				return fmt.Errorf("mise à jour du webhook: status %d — %s", status, string(data))
			}
			return nil
		}
	}
	data, status, err = p.api("POST", hooks, hook)
	if err != nil {
		return err
	}
	if status != 201 {
		return fmt.Errorf("création du webhook: status %d — %s", status, string(data))
	}
	return nil
}

// DeleteRepo supprime le projet. GitLab refuse tant que le container registry
// contient des tags : on les supprime d'abord, puis on réessaie le temps que
// GitLab termine la purge (asynchrone, souvent 2 à 3 min).
func (p *Provider) DeleteRepo(repoPath string) error {
	encoded := url.PathEscape(repoPath)

	data, status, err := p.api("GET", "/projects/"+encoded, nil)
	if err != nil {
		return err
	}
	// 404 : absent ; redirection : le chemin ne désigne plus ce projet (renommé
	// en -deletion_scheduled- par une suppression déjà faite)
	if status == 404 || (status >= 300 && status < 400) {
		return provider.ErrNotFound
	}
	if status != 200 {
		return fmt.Errorf("deleteRepo %s: status %d — %s", repoPath, status, string(data))
	}
	var proj struct {
		ID int `json:"id"`
	}
	if err := json.Unmarshal(data, &proj); err != nil {
		return fmt.Errorf("gitlab deleteRepo : réponse illisible: %w", err)
	}

	data, status, err = p.api("GET", "/projects/"+encoded+"/registry/repositories", nil)
	if err != nil {
		return err
	}
	if status == 200 {
		var repos []struct {
			ID int `json:"id"`
		}
		if err := json.Unmarshal(data, &repos); err != nil {
			return fmt.Errorf("gitlab deleteRepo : réponse illisible: %w", err)
		}
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
			return p.removePermanently(proj.ID)
		case status == 404:
			return provider.ErrNotFound
		case status == 400 && strings.Contains(string(data), "registry") && time.Now().Before(deadline):
			time.Sleep(5 * time.Second)
		default:
			return fmt.Errorf("deleteRepo %s: status %d — %s", repoPath, status, string(data))
		}
	}
}
