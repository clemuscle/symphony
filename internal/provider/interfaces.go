package provider

import (
	"errors"
	"fmt"
	"time"
)

// ErrNotFound : la ressource n'existe pas (ou plus) chez le provider
var ErrNotFound = errors.New("introuvable")

// ─── SCM ──────────────────────────────────────────────────────────────────────

type SCMProvider interface {
	ID() string
	Platform() string // "gitlab", "github"
	CreateRepo(req CreateRepoRequest) (*Repo, error)
	PushFile(repoPath, branch, filePath, content, commitMsg string) error
	PushFiles(repoPath, branch string, files map[string]string, commitMsg string) error
	ListRepos() ([]Repo, error)
	GetFileContent(repoPath, branch, filePath string) (string, error)
	ListFiles(repoPath, branch, dirPath string) ([]string, error)
	DeleteFile(repoPath, branch, filePath, commitMsg string) error // ErrNotFound si absent
	DeleteRepo(repoPath string) error                              // ErrNotFound si absent
}

type CreateRepoRequest struct {
	Name        string
	Description string
	Namespace   string
	Private     bool
}

type Repo struct {
	ID            string `json:"id"`
	Name          string `json:"name"`
	Path          string `json:"path"` // "namespace/name"
	WebURL        string `json:"web_url"`
	HTTPCloneURL  string `json:"http_clone_url"`
	SSHCloneURL   string `json:"ssh_clone_url"`
	DefaultBranch string `json:"default_branch"`
}

// ─── CI ───────────────────────────────────────────────────────────────────────

type CIProvider interface {
	ID() string
	Platform() string                      // plateforme SCM sur laquelle ce CI tourne : "gitlab", "github"
	ConfigFile() string                    // chemin du fichier CI dans le repo (".gitlab-ci.yml", ".github/workflows/ci.yml")
	PipelinesURL(repoWebURL string) string // page des pipelines du repo
	TriggerPipeline(repoPath, ref string, vars map[string]string) (*Pipeline, error)
	GetPipeline(repoPath, pipelineID string) (*Pipeline, error)
	ListPipelines(repoPath string, limit int) ([]Pipeline, error)
}

type Pipeline struct {
	ID         string     `json:"id"`
	Status     string     `json:"status"` // pending, running, success, failed, canceled
	WebURL     string     `json:"web_url"`
	Ref        string     `json:"ref"`
	CreatedAt  time.Time  `json:"created_at"`
	FinishedAt *time.Time `json:"finished_at"`
}

// CheckCompatible vérifie qu'un CI peut tourner sur les repos d'un SCM :
// GitLab CI ne lit pas un repo GitHub, et inversement
func CheckCompatible(scm SCMProvider, ci CIProvider) error {
	if scm.Platform() != ci.Platform() {
		return fmt.Errorf("ci provider %s (%s) incompatible avec le scm provider %s (%s)",
			ci.ID(), ci.Platform(), scm.ID(), scm.Platform())
	}
	return nil
}

// ─── REGISTRY ─────────────────────────────────────────────────────────────────

type RegistryProvider interface {
	ID() string
	GetImageURL(repoPath string) string
	ListTags(repoPath string) ([]string, error)
}

// ─── RUNTIME ──────────────────────────────────────────────────────────────────

type RuntimeProvider interface {
	ID() string
	Type() string // "docker", "kubernetes"
	Deploy(req DeployRequest) (*Deployment, error)
	Stop(deploymentID string) error
	Restart(deploymentID string) error
	Status(deploymentID string) (*Deployment, error)
	List() ([]Deployment, error)
	Logs(deploymentID string, lines int) (string, error)
}

type DeployRequest struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Tag           string            `json:"tag"`
	Port          int               `json:"port"`
	EnvVars       map[string]string `json:"env_vars"`
	HealthCheck   string            `json:"health_check"`
	RestartPolicy string            `json:"restart_policy"`
}

type Deployment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	Status    string    `json:"status"` // running, stopped, error
	URL       string    `json:"url"`
	Port      int       `json:"port"`
	CreatedAt time.Time `json:"created_at"`
}
