package catalog

type Service struct {
	APIVersion string      `yaml:"apiVersion" json:"api_version"`
	Kind       string      `yaml:"kind" json:"kind"`
	Metadata   Metadata    `yaml:"metadata" json:"metadata"`
	Spec       ServiceSpec `yaml:"spec" json:"spec"`
}

type Metadata struct {
	Name      string   `yaml:"name" json:"name"`
	Owner     string   `yaml:"owner" json:"owner"`
	Team      Team     `yaml:"team" json:"team"`
	Tags      []string `yaml:"tags" json:"tags"`
	Tier      string   `yaml:"tier" json:"tier"`           // critical / standard / internal
	Lifecycle string   `yaml:"lifecycle" json:"lifecycle"` // production / staging / deprecated
}

type Team struct {
	Slack string `yaml:"slack" json:"slack"`
	Email string `yaml:"email" json:"email"`
}

type ServiceSpec struct {
	Type         string       `yaml:"type" json:"type"`
	Language     string       `yaml:"language" json:"language"`
	Version      string       `yaml:"version" json:"version"`
	Repo         string       `yaml:"repo" json:"repo"`
	Registry     string       `yaml:"registry" json:"registry"`
	Docs         string       `yaml:"docs" json:"docs"`
	Links        []Link       `yaml:"links" json:"links"`
	Actions      []Action     `yaml:"actions" json:"actions"`
	Dependencies []Dependency `yaml:"dependencies" json:"dependencies"`
	SLO          SLO          `yaml:"slo" json:"slo"`
}

type Link struct {
	Title string `yaml:"title" json:"title"`
	URL   string `yaml:"url" json:"url"`
	Icon  string `yaml:"icon" json:"icon"`
}

type Action struct {
	Name       string  `yaml:"name" json:"name"`
	WebhookURL string  `yaml:"webhook_url" json:"webhook_url"`
	Inputs     []Input `yaml:"inputs" json:"inputs"`
}

type Input struct {
	ID      string `yaml:"id" json:"id"`
	Type    string `yaml:"type" json:"type"`
	Min     *int   `yaml:"min,omitempty" json:"min,omitempty"`
	Max     *int   `yaml:"max,omitempty" json:"max,omitempty"`
	Default string `yaml:"default,omitempty" json:"default,omitempty"`
}

type Dependency struct {
	Service string `yaml:"service" json:"service"`
	Type    string `yaml:"type" json:"type"` // database / cache / api / queue
}

type SLO struct {
	Availability string `yaml:"availability" json:"availability"`
	LatencyP99   string `yaml:"latency_p99" json:"latency_p99"`
}
