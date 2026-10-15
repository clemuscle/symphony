package catalogue

type Service struct {
	APIVersion string      `yaml:"apiVersion" json:"api_version"`
	Kind       string      `yaml:"kind"       json:"kind"`
	Metadata   Metadata    `yaml:"metadata"   json:"metadata"`
	Spec       ServiceSpec `yaml:"spec"       json:"spec"`
}

type Metadata struct {
	Name      string            `yaml:"name"                json:"name"`
	Owner     string            `yaml:"owner,omitempty"     json:"owner"`
	Team      Team              `yaml:"team,omitempty"      json:"team"`
	Tags      []string          `yaml:"tags,omitempty"      json:"tags"`
	Tier      string            `yaml:"tier,omitempty"      json:"tier"`
	Lifecycle string            `yaml:"lifecycle,omitempty" json:"lifecycle"`
	Labels    map[string]string `yaml:"labels,omitempty"    json:"labels,omitempty"`
}

type Team struct {
	Name  string `yaml:"name,omitempty"  json:"name"`
	Slack string `yaml:"slack,omitempty" json:"slack"`
	Email string `yaml:"email,omitempty" json:"email"`
}

type ServiceSpec struct {
	Type         string        `yaml:"type,omitempty"         json:"type"`
	Language     string        `yaml:"language,omitempty"     json:"language"`
	Version      string        `yaml:"version,omitempty"      json:"version"`
	Repo         string        `yaml:"repo,omitempty"         json:"repo"`
	Registry     string        `yaml:"registry,omitempty"     json:"registry"`
	Docs         string        `yaml:"docs,omitempty"         json:"docs"`
	Links        []Link        `yaml:"links,omitempty"        json:"links"`
	Actions      []Action      `yaml:"actions,omitempty"      json:"actions"`
	Dependencies []Dependency  `yaml:"dependencies,omitempty" json:"dependencies"`
	SLO          SLO           `yaml:"slo,omitempty"          json:"slo"`
	Environments []Environment `yaml:"environments,omitempty" json:"environments"`
}

type Link struct {
	Title string `yaml:"title,omitempty" json:"title"`
	URL   string `yaml:"url,omitempty"   json:"url"`
	Icon  string `yaml:"icon,omitempty"  json:"icon"`
}

type Action struct {
	Name        string  `yaml:"name"                  json:"name"`
	Description string  `yaml:"description,omitempty" json:"description"`
	WebhookURL  string  `yaml:"webhook_url,omitempty" json:"webhook_url,omitempty"`
	CIProvider  string  `yaml:"ci_provider,omitempty" json:"ci_provider,omitempty"`
	Pipeline    string  `yaml:"pipeline,omitempty"    json:"pipeline,omitempty"`
	Inputs      []Input `yaml:"inputs,omitempty"      json:"inputs"`
}

type Input struct {
	ID       string   `yaml:"id,omitempty"       json:"id"`
	Label    string   `yaml:"label,omitempty"    json:"label"`
	Type     string   `yaml:"type,omitempty"     json:"type"`
	Required bool     `yaml:"required,omitempty" json:"required"`
	Default  string   `yaml:"default,omitempty"  json:"default,omitempty"`
	Options  []string `yaml:"options,omitempty"  json:"options,omitempty"`
	Min      *int     `yaml:"min,omitempty"      json:"min,omitempty"`
	Max      *int     `yaml:"max,omitempty"      json:"max,omitempty"`
}

type Dependency struct {
	Service string `yaml:"service,omitempty" json:"service"`
	Type    string `yaml:"type,omitempty"    json:"type"`
}

type SLO struct {
	Availability string `yaml:"availability,omitempty" json:"availability"`
	LatencyP99   string `yaml:"latency_p99,omitempty"  json:"latency_p99"`
	ErrorBudget  string `yaml:"error_budget,omitempty" json:"error_budget"`
}

type Environment struct {
	Name string `yaml:"name"           json:"name"`
	URL  string `yaml:"url,omitempty"  json:"url"`
	Type string `yaml:"type,omitempty" json:"type"` // production, staging, dev
	// Variables de l'application dans cet environnement : valeurs, ou
	// secret://<provider>/apps/<projet>/<chemin>#<clé>
	Env map[string]string `yaml:"env,omitempty" json:"env,omitempty"`
}
