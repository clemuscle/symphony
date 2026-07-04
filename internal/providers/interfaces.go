package providers

// ─── 1. SOURCE CODE ───────────────────────────────────────────────────────────

type SCMProvider interface {
	CreateRepo(req RepoRequest) (*RepoResult, error)
	PushFile(projectPath, branch, filePath, content, commitMsg string) error
	ListRepos() ([]Repo, error)
}

type RepoRequest struct {
	Name        string `json:"name"`
	Description string `json:"description"`
	Namespace   string `json:"namespace"`
	Private     bool   `json:"private"`
}

type RepoResult struct {
	ID            int    `json:"id"`
	Path          string `json:"path"`
	WebURL        string `json:"web_url"`
	HTTPCloneURL  string `json:"http_clone_url"`
	SSHCloneURL   string `json:"ssh_clone_url"`
	DefaultBranch string `json:"default_branch"`
}

type Repo struct {
	Name   string `json:"name"`
	Path   string `json:"path"`
	WebURL string `json:"web_url"`
}

// ─── 2. CI AUTOMATION ─────────────────────────────────────────────────────────

type CIProvider interface {
	SetupPipeline(projectPath string, pipeline PipelineConfig) error
	TriggerPipeline(projectPath, ref string, vars map[string]string) (string, error)
	GetPipelineStatus(projectPath, pipelineID string) (string, error)
}

type PipelineConfig struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Language string   `json:"language"`
	Stages   []string `json:"stages"`
	Content  string   `json:"content"`
}

// ─── 3. ARTIFACT STORAGE ──────────────────────────────────────────────────────

type RegistryProvider interface {
	GetRegistryURL(projectPath string) (string, error)
	ListImages(projectPath string) ([]Image, error)
	DeleteImage(projectPath, tag string) error
}

type Image struct {
	Name      string `json:"name"`
	Tag       string `json:"tag"`
	Size      int64  `json:"size"`
	CreatedAt string `json:"created_at"`
}

// ─── 4. DEPLOYMENT ────────────────────────────────────────────────────────────

type DeployProvider interface {
	Stop(deploymentID string) error
	Status(deploymentID string) (string, error)
	List() ([]Deployment, error)
}

type Deployment struct {
	ID          string `json:"id"`
	ProjectName string `json:"project_name"`
	Image       string `json:"image"`
	Status      string `json:"status"`
	URL         string `json:"url"`
}
