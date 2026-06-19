package providers

// ─── 1. SOURCE CODE ───────────────────────────────────────────────────────────

type SCMProvider interface {
	CreateRepo(req RepoRequest) (*RepoResult, error)
	PushFile(projectPath, branch, filePath, content, commitMsg string) error
	ListRepos() ([]Repo, error)
	Scaffold(repo *RepoResult, cfg ScaffoldConfig) error
}

type RepoRequest struct {
	Name        string
	Description string
	Namespace   string
	Private     bool
}

type RepoResult struct {
	ID            int
	Path          string
	WebURL        string
	HTTPCloneURL  string
	SSHCloneURL   string
	DefaultBranch string
}

type Repo struct {
	Name   string
	Path   string
	WebURL string
}

type ScaffoldConfig struct {
	Name        string
	Description string
	Language    string
	Type        string
	Port        int
}

// ─── 2. CI AUTOMATION ─────────────────────────────────────────────────────────

type CIProvider interface {
	SetupPipeline(projectPath string, pipeline PipelineConfig) error
	TriggerPipeline(projectPath, ref string, vars map[string]string) (string, error)
	GetPipelineStatus(projectPath, pipelineID string) (string, error)
	EnsureRunner(name, executorType string) error
}

type PipelineConfig struct {
	Name     string
	Type     string
	Language string
	Stages   []string
}

// ─── 3. ARTIFACT STORAGE ──────────────────────────────────────────────────────

type RegistryProvider interface {
	GetRegistryURL(projectPath string) (string, error)
	ListImages(projectPath string) ([]Image, error)
	DeleteImage(projectPath, tag string) error
}

type Image struct {
	Name      string
	Tag       string
	Size      int64
	CreatedAt string
}

// ─── 4. DEPLOYMENT ────────────────────────────────────────────────────────────

type DeployProvider interface {
	Deploy(req DeployRequest) (*DeployResult, error)
	Stop(deploymentID string) error
	Status(deploymentID string) (string, error)
	List() ([]Deployment, error)
}

type DeployRequest struct {
	ProjectName string
	Image       string
	Port        int
	EnvVars     map[string]string
	HealthCheck string
}

type DeployResult struct {
	DeploymentID string
	URL          string
	Status       string
}

type Deployment struct {
	ID          string
	ProjectName string
	Image       string
	Status      string
	URL         string
}
