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
	// EnsureWebhook abonne url aux événements de pipeline du repo, avec secret
	// (vérifié à la réception) ; si url y est déjà abonnée, son secret est mis
	// à jour (rotation). ErrUnsupported si le provider ne sait pas le faire.
	EnsureWebhook(repoPath, url, secret string) error
	// Check : le provider répond et accepte ses identifiants (check.go)
	Check() (string, error)
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
	// Check : le provider répond et accepte ses identifiants (check.go)
	Check() (string, error)
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
	// DeleteImages supprime les images du projet, quand la suppression du
	// repo ne s'en charge pas (GHCR). Sans effet s'il n'y en a pas.
	DeleteImages(repoPath string) error
	// Check : le provider répond et accepte ses identifiants (check.go)
	Check() (string, error)
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
	// SecretRefs : le runtime transmet lui-même les secrets à l'application
	// (DeployRequest.SecretEnv, des références : Symphony ne lit pas leur
	// valeur) ; sinon Symphony les résout et les passe dans EnvVars. Un runtime
	// qui écrit dans Git (argocd) ne doit jamais recevoir de valeur.
	SecretRefs() bool
	// Remove supprime les déploiements du projet, dans tous ses environnements,
	// et tout ce qu'ils ont créé (conteneurs et images, ou Applications,
	// ressources et manifests). Sans effet s'il n'y en a pas. La suppression
	// peut finir après le retour : List ne les montre plus quand elle est
	// terminée.
	Remove(project string) error
	// Check : le provider répond et accepte ses identifiants (check.go)
	Check() (string, error)
}

type DeployRequest struct {
	// Name : nom du déploiement (conteneur, Application), unique par runtime :
	// DeploymentName(Project, Environment)
	Name        string            `json:"name"`
	Project     string            `json:"project"`
	Environment string            `json:"environment"` // vide : sans environnements
	Image       string            `json:"image"`
	Tag         string            `json:"tag"`
	Port        int               `json:"port"`      // port écouté par l'appli dans le container
	HostPort    int               `json:"host_port"` // 0 : celui du déploiement précédent, sinon choisi par le runtime
	EnvVars     map[string]string `json:"env_vars"`
	// Variables dont la valeur est un secret, quand SecretRefs() : références
	// seules, lues dans le cluster par le runtime
	SecretEnv     map[string]SecretRef `json:"-"`
	HealthCheck   string               `json:"health_check"`
	RestartPolicy string               `json:"restart_policy"`
	RegistryAuth  *RegistryAuth        `json:"-"` // renseigné par l'API, jamais lu du body
	// Manifests : chart Helm ou base Kustomize fournis par la stack du projet,
	// lus dans son repo ; nil : le runtime génère les siens. Un runtime qui
	// n'en a pas l'usage (docker) les ignore.
	Manifests *Manifests `json:"-"`
}

// Manifests : les fichiers d'un chart Helm ou d'une base Kustomize
type Manifests struct {
	Kind  string            // "helm" ou "kustomize"
	Files map[string]string // chemin relatif au chart ou à la base → contenu
}

type Deployment struct {
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Project     string    `json:"project"`     // projet déployé (labels du runtime)
	Environment string    `json:"environment"` // vide : déploiement sans environnement
	Image       string    `json:"image"`
	Status      string    `json:"status"` // running, deploying, stopped, error
	URL         string    `json:"url"`
	Port        int       `json:"port"` // port publié côté hôte (celui de l'URL)
	CreatedAt   time.Time `json:"created_at"`
	// Runtime qui porte le déploiement, renseigné par l'API
	Provider string `json:"provider"`
	// État GitOps (runtime argocd) : synchronisation avec Git et santé
	Sync   string `json:"sync,omitempty"`   // Synced, OutOfSync
	Health string `json:"health,omitempty"` // Healthy, Progressing, Degraded…
}

// DeploymentName : nom du déploiement d'un projet dans un environnement
// (<projet>-<environnement>), le projet seul sans environnement
func DeploymentName(project, environment string) string {
	if environment == "" {
		return project
	}
	return project + "-" + environment
}

// ─── SECRETS ──────────────────────────────────────────────────────────────────

// SecretsProvider lit des secrets (Vault, variables d'environnement…). La
// configuration y fait référence par secret://<id>/<path>#<key>.
type SecretsProvider interface {
	ID() string
	Type() string // "env", "vault"
	// Get renvoie la valeur key du secret path ; ErrNotFound si l'un ou
	// l'autre est absent. Pour env, path est le nom de la variable.
	Get(path, key string) (string, error)
	// Check : le provider répond et accepte ses identifiants (check.go)
	Check() (string, error)
}

// ─── IDENTITY ─────────────────────────────────────────────────────────────────

// IdentityProvider : fournisseur d'identité OIDC (connexion des utilisateurs),
// et annuaire quand il en a un (Keycloak)
type IdentityProvider interface {
	ID() string
	Type() string // "oidc", "keycloak"
	OIDC() OIDCSettings
	// ListGroups : groupes de l'annuaire (équipes), triés par nom.
	// ErrUnsupported si le provider n'a pas d'annuaire.
	ListGroups() ([]Group, error)
	// Check : le provider répond et accepte ses identifiants (check.go)
	Check() (string, error)
}

// OIDCSettings : ce dont la connexion OIDC a besoin
type OIDCSettings struct {
	Issuer       string // tel que le voit le navigateur (claim iss)
	DiscoveryURL string // où Symphony lit la découverte, si elle diffère de l'issuer
	ClientID     string
	ClientSecret string // vide : client public (PKCE)
	RedirectURL  string // vide : déduite de la requête
	GroupsClaim  string // défaut : groups
}

type Group struct {
	Name string `json:"name"`
	Path string `json:"path"` // chemin complet dans l'annuaire (/parent/enfant)
}
