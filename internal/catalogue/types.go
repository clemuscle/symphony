package catalogue

type Service struct {
	APIVersion string      `yaml:"apiVersion" json:"api_version"`
	Kind       string      `yaml:"kind"       json:"kind"`
	Metadata   Metadata    `yaml:"metadata"   json:"metadata"`
	Spec       ServiceSpec `yaml:"spec"       json:"spec"`
}

type Metadata struct {
	Name      string            `yaml:"name"      json:"name"`
	Owner     string            `yaml:"owner"     json:"owner"`
	Team      Team              `yaml:"team"      json:"team"`
	Tags      []string          `yaml:"tags"      json:"tags"`
	Tier      string            `yaml:"tier"      json:"tier"`
	Lifecycle string            `yaml:"lifecycle" json:"lifecycle"`
	Labels    map[string]string `yaml:"labels"    json:"labels,omitempty"`
}

type Team struct {
	Name  string `yaml:"name"  json:"name"`
	Slack string `yaml:"slack" json:"slack"`
	Email string `yaml:"email" json:"email"`
}

type ServiceSpec struct {
	Type         string        `yaml:"type"         json:"type"`
	Language     string        `yaml:"language"     json:"language"`
	Version      string        `yaml:"version"      json:"version"`
	Repo         string        `yaml:"repo"         json:"repo"`
	Registry     string        `yaml:"registry"     json:"registry"`
	Docs         string        `yaml:"docs"         json:"docs"`
	Links        []Link        `yaml:"links"        json:"links"`
	Actions      []Action      `yaml:"actions"      json:"actions"`
	Dependencies []Dependency  `yaml:"dependencies" json:"dependencies"`
	SLO          SLO           `yaml:"slo"          json:"slo"`
	Environments []Environment `yaml:"environments" json:"environments"`
}

type Link struct {
	Title string `yaml:"title" json:"title"`
	URL   string `yaml:"url"   json:"url"`
	Icon  string `yaml:"icon"  json:"icon"`
}

type Action struct {
	Name        string  `yaml:"name"        json:"name"`
	Description string  `yaml:"description" json:"description"`
	WebhookURL  string  `yaml:"webhook_url" json:"webhook_url,omitempty"`
	CIProvider  string  `yaml:"ci_provider" json:"ci_provider,omitempty"`
	Pipeline    string  `yaml:"pipeline"    json:"pipeline,omitempty"`
	Inputs      []Input `yaml:"inputs"      json:"inputs"`
}

type Input struct {
	ID       string   `yaml:"id"          json:"id"`
	Label    string   `yaml:"label"       json:"label"`
	Type     string   `yaml:"type"        json:"type"`
	Required bool     `yaml:"required"    json:"required"`
	Default  string   `yaml:"default"     json:"default,omitempty"`
	Options  []string `yaml:"options"     json:"options,omitempty"`
	Min      *int     `yaml:"min"         json:"min,omitempty"`
	Max      *int     `yaml:"max"         json:"max,omitempty"`
}

type Dependency struct {
	Service string `yaml:"service" json:"service"`
	Type    string `yaml:"type"    json:"type"`
}

type SLO struct {
	Availability string `yaml:"availability" json:"availability"`
	LatencyP99   string `yaml:"latency_p99"  json:"latency_p99"`
	ErrorBudget  string `yaml:"error_budget" json:"error_budget"`
}

type Environment struct {
	Name string `yaml:"name" json:"name"`
	URL  string `yaml:"url"  json:"url"`
	Type string `yaml:"type" json:"type"` // production, staging, dev
}
