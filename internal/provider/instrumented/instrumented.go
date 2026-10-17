// Package instrumented enveloppe les providers pour mesurer leurs appels
// (durée, erreurs) dans les métriques Prometheus. Les méthodes sans appel
// réseau (ID, Platform, ConfigFile…) sont transmises telles quelles ; les
// erreurs sont renvoyées inchangées (errors.Is(err, provider.ErrNotFound) marche).
package instrumented

import (
	"errors"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
)

// call mesure f, un appel de la méthode method du provider p
func call[T any](kind, id, method string, f func() (T, error)) (T, error) {
	start := time.Now()
	v, err := f()
	metrics.ObserveProviderCall(kind, id, method, start, err)
	record(kind, id, method, start, err)
	return v, err
}

// LastCall : le dernier appel d'un provider, pour la page Administration.
// Error est vide si l'appel a réussi ; une ressource introuvable ou une
// opération non prise en charge ne sont pas des pannes du provider.
type LastCall struct {
	Method string    `json:"method"`
	At     time.Time `json:"at"`
	Error  string    `json:"error,omitempty"`
}

var (
	lastMu    sync.Mutex
	lastCalls = map[string]LastCall{}
)

func record(kind, id, method string, at time.Time, err error) {
	c := LastCall{Method: method, At: at}
	if err != nil && !errors.Is(err, provider.ErrNotFound) && !errors.Is(err, provider.ErrUnsupported) {
		c.Error = err.Error()
	}
	lastMu.Lock()
	lastCalls[kind+"/"+id] = c
	lastMu.Unlock()
}

// Last : le dernier appel du provider id de la section kind (scm, ci…)
func Last(kind, id string) (LastCall, bool) {
	lastMu.Lock()
	defer lastMu.Unlock()
	c, ok := lastCalls[kind+"/"+id]
	return c, ok
}

func call0(kind, id, method string, f func() error) error {
	_, err := call(kind, id, method, func() (struct{}, error) { return struct{}{}, f() })
	return err
}

// ─── SCM ──────────────────────────────────────────────────────────────────────

type scm struct{ provider.SCMProvider }

func SCM(p provider.SCMProvider) provider.SCMProvider { return scm{p} }

func (s scm) Check() (string, error) {
	return call("scm", s.ID(), "Check", s.SCMProvider.Check)
}

func (s scm) CreateRepo(req provider.CreateRepoRequest) (*provider.Repo, error) {
	return call("scm", s.ID(), "CreateRepo", func() (*provider.Repo, error) { return s.SCMProvider.CreateRepo(req) })
}

func (s scm) PushFile(repoPath, branch, filePath, content, commitMsg string) error {
	return call0("scm", s.ID(), "PushFile", func() error { return s.SCMProvider.PushFile(repoPath, branch, filePath, content, commitMsg) })
}

func (s scm) PushFiles(repoPath, branch string, files map[string]string, commitMsg string) error {
	return call0("scm", s.ID(), "PushFiles", func() error { return s.SCMProvider.PushFiles(repoPath, branch, files, commitMsg) })
}

func (s scm) ListRepos() ([]provider.Repo, error) {
	return call("scm", s.ID(), "ListRepos", s.SCMProvider.ListRepos)
}

func (s scm) GetFileContent(repoPath, branch, filePath string) (string, error) {
	return call("scm", s.ID(), "GetFileContent", func() (string, error) { return s.SCMProvider.GetFileContent(repoPath, branch, filePath) })
}

func (s scm) ListFiles(repoPath, branch, dirPath string) ([]string, error) {
	return call("scm", s.ID(), "ListFiles", func() ([]string, error) { return s.SCMProvider.ListFiles(repoPath, branch, dirPath) })
}

func (s scm) DeleteFile(repoPath, branch, filePath, commitMsg string) error {
	return call0("scm", s.ID(), "DeleteFile", func() error { return s.SCMProvider.DeleteFile(repoPath, branch, filePath, commitMsg) })
}

func (s scm) EnsureWebhook(repoPath, url, secret string) error {
	return call0("scm", s.ID(), "EnsureWebhook", func() error { return s.SCMProvider.EnsureWebhook(repoPath, url, secret) })
}

func (s scm) DeleteRepo(repoPath string) error {
	return call0("scm", s.ID(), "DeleteRepo", func() error { return s.SCMProvider.DeleteRepo(repoPath) })
}

// ─── CI ───────────────────────────────────────────────────────────────────────

type ci struct{ provider.CIProvider }

func CI(p provider.CIProvider) provider.CIProvider { return ci{p} }

func (c ci) Check() (string, error) {
	return call("ci", c.ID(), "Check", c.CIProvider.Check)
}

func (c ci) TriggerPipeline(repoPath, ref string, vars map[string]string) (*provider.Pipeline, error) {
	return call("ci", c.ID(), "TriggerPipeline", func() (*provider.Pipeline, error) {
		return c.CIProvider.TriggerPipeline(repoPath, ref, vars)
	})
}

func (c ci) GetPipeline(repoPath, pipelineID string) (*provider.Pipeline, error) {
	return call("ci", c.ID(), "GetPipeline", func() (*provider.Pipeline, error) {
		return c.CIProvider.GetPipeline(repoPath, pipelineID)
	})
}

func (c ci) ListPipelines(repoPath string, limit int) ([]provider.Pipeline, error) {
	return call("ci", c.ID(), "ListPipelines", func() ([]provider.Pipeline, error) {
		return c.CIProvider.ListPipelines(repoPath, limit)
	})
}

// ─── REGISTRY ─────────────────────────────────────────────────────────────────

type registry struct{ provider.RegistryProvider }

func Registry(p provider.RegistryProvider) provider.RegistryProvider { return registry{p} }

func (r registry) Check() (string, error) {
	return call("registry", r.ID(), "Check", r.RegistryProvider.Check)
}

func (r registry) ListTags(repoPath string) ([]string, error) {
	return call("registry", r.ID(), "ListTags", func() ([]string, error) { return r.RegistryProvider.ListTags(repoPath) })
}

func (r registry) DeleteImages(repoPath string) error {
	return call0("registry", r.ID(), "DeleteImages", func() error { return r.RegistryProvider.DeleteImages(repoPath) })
}

// ─── RUNTIME ──────────────────────────────────────────────────────────────────

type runtime struct{ provider.RuntimeProvider }

func Runtime(p provider.RuntimeProvider) provider.RuntimeProvider { return runtime{p} }

func (r runtime) Check() (string, error) {
	return call("runtime", r.ID(), "Check", r.RuntimeProvider.Check)
}

func (r runtime) Deploy(req provider.DeployRequest) (*provider.Deployment, error) {
	return call("runtime", r.ID(), "Deploy", func() (*provider.Deployment, error) { return r.RuntimeProvider.Deploy(req) })
}

func (r runtime) Stop(deploymentID string) error {
	return call0("runtime", r.ID(), "Stop", func() error { return r.RuntimeProvider.Stop(deploymentID) })
}

func (r runtime) Restart(deploymentID string) error {
	return call0("runtime", r.ID(), "Restart", func() error { return r.RuntimeProvider.Restart(deploymentID) })
}

func (r runtime) Status(deploymentID string) (*provider.Deployment, error) {
	return call("runtime", r.ID(), "Status", func() (*provider.Deployment, error) { return r.RuntimeProvider.Status(deploymentID) })
}

func (r runtime) List() ([]provider.Deployment, error) {
	return call("runtime", r.ID(), "List", r.RuntimeProvider.List)
}

func (r runtime) Logs(deploymentID string, lines int) (string, error) {
	return call("runtime", r.ID(), "Logs", func() (string, error) { return r.RuntimeProvider.Logs(deploymentID, lines) })
}

func (r runtime) Remove(name string) error {
	return call0("runtime", r.ID(), "Remove", func() error { return r.RuntimeProvider.Remove(name) })
}

// ─── SECRETS ──────────────────────────────────────────────────────────────────

type secrets struct{ provider.SecretsProvider }

func Secrets(p provider.SecretsProvider) provider.SecretsProvider { return secrets{p} }

func (s secrets) Check() (string, error) {
	return call("secrets", s.ID(), "Check", s.SecretsProvider.Check)
}

func (s secrets) Get(path, key string) (string, error) {
	return call("secrets", s.ID(), "Get", func() (string, error) { return s.SecretsProvider.Get(path, key) })
}

// ─── IDENTITY ─────────────────────────────────────────────────────────────────

type identity struct{ provider.IdentityProvider }

func Identity(p provider.IdentityProvider) provider.IdentityProvider { return identity{p} }

func (i identity) Check() (string, error) {
	return call("identity", i.ID(), "Check", i.IdentityProvider.Check)
}

func (i identity) ListGroups() ([]provider.Group, error) {
	return call("identity", i.ID(), "ListGroups", i.IdentityProvider.ListGroups)
}
