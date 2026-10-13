package provider

import (
	"errors"
	"fmt"
	"time"
)

// ErrNotFound : la ressource n'existe pas (ou plus) chez le provider
var ErrNotFound = errors.New("introuvable")

// ErrUnsupported : le provider ne sait pas faire cette opération
var ErrUnsupported = errors.New("non pris en charge par ce provider")

// ─── SCM ──────────────────────────────────────────────────────────────────────

type SCMProvider interface {
	ID() string
	Platform() string // "gitlab", "github"
	CreateRepo(req CreateRepoRequest) (*Repo, error)
	PushFile(repoPath, branch, filePath, content, commitMsg string) error
	PushFiles(repoPath, branch string, files map[string]string, commitMsg string) error
	ListRepos() ([]Repo, error)
	GetFileContent(repoPath, branch, filePath string) (string, error)
	ListFiles(repoPath, branch, dirPath string) ([]string, error)  // ErrNotFound si le dossier est absent
	DeleteFile(repoPath, branch, filePath, commitMsg string) error // ErrNotFound si absent
	DeleteRepo(repoPath string) error                              // ErrNotFound si absent
	// CreateWebhook abonne url aux événements de pipeline du repo, avec secret
	// (vérifié à la réception) ; sans effet si url y est déjà abonnée.
	// ErrUnsupported si le provider ne sait pas le faire.
	CreateWebhook(repoPath, url, secret string) error
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
	Auth() *RegistryAuth // identifiants de pull, nil si aucun
}

// RegistryAuth : identifiants transmis au runtime pour puller une image
type RegistryAuth struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	ServerAddress string `json:"serveraddress"`
}

// ─── RUNTIME ──────────────────────────────────────────────────────────────────

type RuntimeProvider interface {
	ID() string
	Type() string // "docker", "argocd"
	Deploy(req DeployRequest) (*Deployment, error)
	Stop(deploymentID string) error
	Restart(deploymentID string) error
	Status(deploymentID string) (*Deployment, error)
	List() ([]Deployment, error)
	Logs(deploymentID string, lines int) (string, error)
	// Remove supprime le déploiement du service name et tout ce qu'il a créé
	// (conteneurs et images, ou Application, ressources et manifests). Sans
	// effet s'il n'existe pas. La suppression peut finir après le retour :
	// Status(name) répond ErrNotFound quand elle est terminée.
	Remove(name string) error
}

type DeployRequest struct {
	Name          string            `json:"name"`
	Image         string            `json:"image"`
	Tag           string            `json:"tag"`
	Port          int               `json:"port"`      // port écouté par l'appli dans le container
	HostPort      int               `json:"host_port"` // 0 : celui du déploiement précédent, sinon choisi par le runtime
	EnvVars       map[string]string `json:"env_vars"`
	HealthCheck   string            `json:"health_check"`
	RestartPolicy string            `json:"restart_policy"`
	RegistryAuth  *RegistryAuth     `json:"-"` // renseigné par l'API, jamais lu du body
}

type Deployment struct {
	ID        string    `json:"id"`
	Name      string    `json:"name"`
	Image     string    `json:"image"`
	Status    string    `json:"status"` // running, deploying, stopped, error
	URL       string    `json:"url"`
	Port      int       `json:"port"` // port publié côté hôte (celui de l'URL)
	CreatedAt time.Time `json:"created_at"`
	// Runtime qui porte le déploiement, renseigné par l'API
	Provider string `json:"provider"`
	// État GitOps (runtime argocd) : synchronisation avec Git et santé
	Sync   string `json:"sync,omitempty"`   // Synced, OutOfSync
	Health string `json:"health,omitempty"` // Healthy, Progressing, Degraded…
}
