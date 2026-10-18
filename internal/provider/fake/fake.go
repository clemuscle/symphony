// Package fake : providers en mémoire pour les tests d'intégration (API,
// tâches) sans GitLab, CI, registry ni Docker. Une pipeline lancée par un
// push réussit aussitôt et publie l'image du repo.
package fake

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

// ─── SCM + CI ─────────────────────────────────────────────────────────────────

// Forge : SCM et CI d'une même plateforme ("gitlab"). Chaque push sur un repo
// crée une pipeline réussie, et un tag d'image dans Registry si fourni.
type Forge struct {
	mu        sync.Mutex
	files     map[string]map[string]string // repo → chemin → contenu
	pipelines map[string][]provider.Pipeline
	webhooks  map[string]string // repo → url
	Registry  *Registry
	pushes    int
}

func NewForge(reg *Registry) *Forge {
	return &Forge{
		files:     map[string]map[string]string{},
		pipelines: map[string][]provider.Pipeline{},
		webhooks:  map[string]string{},
		Registry:  reg,
	}
}

func (f *Forge) SCM() provider.SCMProvider { return forgeSCM{f} }
func (f *Forge) CI() provider.CIProvider   { return forgeCI{f} }

// File : contenu d'un fichier d'un repo (tests)
func (f *Forge) File(repo, path string) (string, bool) {
	f.mu.Lock()
	defer f.mu.Unlock()
	c, ok := f.files[repo][path]
	return c, ok
}

// Repos : repos existants (tests)
func (f *Forge) Repos() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	var repos []string
	for r := range f.files {
		repos = append(repos, r)
	}
	sort.Strings(repos)
	return repos
}

// Seed crée un repo existant, hors de Symphony, avec ses fichiers : une
// pipeline réussie et une image, comme pour un push (tests de la découverte)
func (f *Forge) Seed(repo string, files map[string]string) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.push(repo, files)
}

// push enregistre des fichiers et lance une pipeline réussie (appelé sous f.mu)
func (f *Forge) push(repo string, files map[string]string) {
	if f.files[repo] == nil {
		f.files[repo] = map[string]string{}
	}
	for p, c := range files {
		f.files[repo][p] = c
	}
	f.pushes++
	id := fmt.Sprint(f.pushes)
	now := time.Now()
	f.pipelines[repo] = append([]provider.Pipeline{{
		ID: id, Status: "success", Ref: "main", CreatedAt: now, FinishedAt: &now,
		WebURL: "http://forge/" + repo + "/-/pipelines/" + id,
	}}, f.pipelines[repo]...)
	if f.Registry != nil {
		f.Registry.publish(repo, "sha"+id)
	}
}

type forgeSCM struct{ f *Forge }

func (s forgeSCM) ID() string       { return "gitlab" }
func (s forgeSCM) Platform() string { return "gitlab" }

func (s forgeSCM) CreateRepo(req provider.CreateRepoRequest) (*provider.Repo, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	path := strings.Trim(req.Namespace+"/"+req.Name, "/")
	if !strings.Contains(path, "/") {
		path = "apps/" + path
	}
	if _, ok := s.f.files[path]; ok {
		return nil, fmt.Errorf("repo %s existe déjà", path)
	}
	s.f.files[path] = map[string]string{}
	return &provider.Repo{ID: path, Name: req.Name, Path: path, WebURL: "http://forge/" + path,
		HTTPCloneURL: "http://forge/" + path + ".git", DefaultBranch: "main"}, nil
}

func (s forgeSCM) PushFile(repo, _, path, content, _ string) error {
	return s.PushFiles(repo, "main", map[string]string{path: content}, "")
}

func (s forgeSCM) PushFiles(repo, _ string, files map[string]string, msg string) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if _, ok := s.f.files[repo]; !ok {
		return provider.ErrNotFound
	}
	if strings.Contains(msg, "[skip ci]") {
		for p, c := range files {
			s.f.files[repo][p] = c
		}
		return nil
	}
	s.f.push(repo, files)
	return nil
}

func (s forgeSCM) ListRepos() ([]provider.Repo, error) {
	var out []provider.Repo
	for _, r := range s.f.Repos() {
		out = append(out, provider.Repo{Path: r})
	}
	return out, nil
}

func (s forgeSCM) ListGroupRepos(group string) ([]provider.Repo, error) {
	out := []provider.Repo{}
	for _, r := range s.f.Repos() {
		if strings.HasPrefix(r, group+"/") {
			out = append(out, provider.Repo{ID: r, Name: r[strings.LastIndex(r, "/")+1:], Path: r,
				WebURL: "http://forge/" + r, DefaultBranch: "main"})
		}
	}
	if len(out) == 0 {
		return nil, provider.ErrNotFound
	}
	return out, nil
}

func (s forgeSCM) GetFileContent(repo, _, path string) (string, error) {
	if c, ok := s.f.File(repo, path); ok {
		return c, nil
	}
	return "", provider.ErrNotFound
}

func (s forgeSCM) ListFiles(repo, _, dir string) ([]string, error) {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	var out []string
	for p := range s.f.files[repo] {
		if strings.HasPrefix(p, strings.TrimSuffix(dir, "/")+"/") {
			out = append(out, p)
		}
	}
	if len(out) == 0 {
		return nil, provider.ErrNotFound
	}
	sort.Strings(out)
	return out, nil
}

func (s forgeSCM) DeleteFile(repo, _, path, _ string) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if _, ok := s.f.files[repo][path]; !ok {
		return provider.ErrNotFound
	}
	delete(s.f.files[repo], path)
	return nil
}

func (s forgeSCM) DeleteRepo(repo string) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	if _, ok := s.f.files[repo]; !ok {
		return provider.ErrNotFound
	}
	delete(s.f.files, repo)
	delete(s.f.pipelines, repo)
	return nil
}

func (s forgeSCM) EnsureWebhook(repo, url, _ string) error {
	s.f.mu.Lock()
	defer s.f.mu.Unlock()
	s.f.webhooks[repo] = url
	return nil
}

type forgeCI struct{ f *Forge }

func (c forgeCI) ID() string                            { return "gitlab-ci" }
func (c forgeCI) Platform() string                      { return "gitlab" }
func (c forgeCI) ConfigFile() string                    { return ".gitlab-ci.yml" }
func (c forgeCI) PipelinesURL(repoWebURL string) string { return repoWebURL + "/-/pipelines" }

func (c forgeCI) TriggerPipeline(repo, _ string, _ map[string]string) (*provider.Pipeline, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	if _, ok := c.f.files[repo]; !ok {
		return nil, provider.ErrNotFound
	}
	c.f.push(repo, nil)
	p := c.f.pipelines[repo][0]
	return &p, nil
}

func (c forgeCI) GetPipeline(repo, id string) (*provider.Pipeline, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	for _, p := range c.f.pipelines[repo] {
		if p.ID == id {
			return &p, nil
		}
	}
	return nil, provider.ErrNotFound
}

func (c forgeCI) ListPipelines(repo string, limit int) ([]provider.Pipeline, error) {
	c.f.mu.Lock()
	defer c.f.mu.Unlock()
	list := c.f.pipelines[repo]
	if limit > 0 && len(list) > limit {
		list = list[:limit]
	}
	return append([]provider.Pipeline(nil), list...), nil
}

// ─── Registry ─────────────────────────────────────────────────────────────────

type Registry struct {
	mu   sync.Mutex
	tags map[string][]string // repo → tags, le plus récent d'abord
}

func NewRegistry() *Registry { return &Registry{tags: map[string][]string{}} }

func (r *Registry) publish(repo, tag string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.tags[repo] = append([]string{tag}, r.tags[repo]...)
}

func (r *Registry) ID() string                     { return "registry" }
func (r *Registry) GetImageURL(repo string) string { return "registry.test/" + repo }
func (r *Registry) Auth() *provider.RegistryAuth   { return nil }
func (r *Registry) ListTags(repo string) ([]string, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string{}, r.tags[repo]...), nil
}

func (r *Registry) DeleteImages(repo string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	delete(r.tags, repo)
	return nil
}

// ─── Runtime ──────────────────────────────────────────────────────────────────

// Runtime : déploiements en mémoire, aussitôt en marche
type Runtime struct {
	id   string
	refs bool
	mu   sync.Mutex
	deps map[string]provider.Deployment
	// LastEnv : variables du dernier déploiement, par nom (tests)
	LastEnv       map[string]map[string]string
	LastSecretEnv map[string]map[string]provider.SecretRef
	// LastInfra : infra du dernier déploiement, par nom ; Kind : le type
	// annoncé (argocd pour simuler Kubernetes), fake par défaut
	LastInfra map[string]*provider.Infra
	Kind      string
}

// NewRuntime : secretRefs dit si le runtime reçoit les secrets en références
// (comme argocd) ou en valeurs (comme docker)
func NewRuntime(id string, secretRefs bool) *Runtime {
	return &Runtime{id: id, refs: secretRefs, deps: map[string]provider.Deployment{},
		LastEnv: map[string]map[string]string{}, LastSecretEnv: map[string]map[string]provider.SecretRef{},
		LastInfra: map[string]*provider.Infra{}}
}

func (r *Runtime) ID() string { return r.id }
func (r *Runtime) Type() string {
	if r.Kind != "" {
		return r.Kind
	}
	return "fake"
}
func (r *Runtime) SecretRefs() bool { return r.refs }

func (r *Runtime) Deploy(req provider.DeployRequest) (*provider.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	image := req.Image
	if req.Tag != "" {
		image += ":" + req.Tag
	}
	d := provider.Deployment{
		ID: req.Name, Name: req.Name, Project: req.Project, Environment: req.Environment,
		Image: image, Status: "running", URL: "http://" + req.Name + ".test", CreatedAt: time.Now(),
	}
	r.deps[req.Name] = d
	r.LastEnv[req.Name] = req.EnvVars
	r.LastSecretEnv[req.Name] = req.SecretEnv
	r.LastInfra[req.Name] = req.Infra
	return &d, nil
}

func (r *Runtime) set(id, status string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.deps[id]
	if !ok {
		return provider.ErrNotFound
	}
	d.Status = status
	r.deps[id] = d
	return nil
}

func (r *Runtime) Stop(id string) error    { return r.set(id, "stopped") }
func (r *Runtime) Restart(id string) error { return r.set(id, "running") }

func (r *Runtime) Status(id string) (*provider.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	d, ok := r.deps[id]
	if !ok {
		return nil, provider.ErrNotFound
	}
	return &d, nil
}

func (r *Runtime) List() ([]provider.Deployment, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	out := []provider.Deployment{}
	for _, d := range r.deps {
		out = append(out, d)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func (r *Runtime) Logs(id string, _ int) (string, error) {
	if _, err := r.Status(id); err != nil {
		return "", err
	}
	return "démarrage\nprêt\n", nil
}

func (r *Runtime) Remove(project string) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	for name, d := range r.deps {
		if d.Project == project || name == project {
			delete(r.deps, name)
		}
	}
	return nil
}

var (
	_ provider.SCMProvider      = forgeSCM{}
	_ provider.CIProvider       = forgeCI{}
	_ provider.RegistryProvider = (*Registry)(nil)
	_ provider.RuntimeProvider  = (*Runtime)(nil)
)

func (forgeSCM) Check() (string, error)  { return "forge simulée", nil }
func (forgeCI) Check() (string, error)   { return "forge simulée", nil }
func (*Registry) Check() (string, error) { return "registry simulé", nil }
func (*Runtime) Check() (string, error)  { return "runtime simulé", nil }
