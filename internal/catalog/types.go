package catalog

type Service struct {
	APIVersion string      `yaml:"apiVersion" json:"APIVersion"`
	Kind       string      `yaml:"kind" json:"Kind"`
	Metadata   Metadata    `yaml:"metadata" json:"Metadata"`
	Spec       ServiceSpec `yaml:"spec" json:"Spec"`
}

type Metadata struct {
	Name      string   `yaml:"name" json:"Name"`
	Owner     string   `yaml:"owner" json:"Owner"`
	Team      Team     `yaml:"team" json:"Team"`
	Tags      []string `yaml:"tags" json:"Tags"`
	Tier      string   `yaml:"tier" json:"Tier"`           // critical / standard / internal
	Lifecycle string   `yaml:"lifecycle" json:"Lifecycle"` // production / staging / deprecated
}

type Team struct {
	Slack string `yaml:"slack" json:"Slack"`
	Email string `yaml:"email" json:"Email"`
}

type ServiceSpec struct {
	Type         string       `yaml:"type" json:"Type"`
	Language     string       `yaml:"language" json:"Language"`
	Version      string       `yaml:"version" json:"Version"`
	Repo         string       `yaml:"repo" json:"Repo"`
	Registry     string       `yaml:"registry" json:"Registry"`
	Docs         string       `yaml:"docs" json:"Docs"`
	Links        []Link       `yaml:"links" json:"Links"`
	Actions      []Action     `yaml:"actions" json:"Actions"`
	Dependencies []Dependency `yaml:"dependencies" json:"Dependencies"`
	SLO          SLO          `yaml:"slo" json:"SLO"`
}

type Link struct {
	Title string `yaml:"title" json:"Title"`
	URL   string `yaml:"url" json:"URL"`
	Icon  string `yaml:"icon" json:"Icon"`
}

type Action struct {
	Name       string  `yaml:"name" json:"Name"`
	WebhookURL string  `yaml:"webhook_url" json:"WebhookURL"`
	Inputs     []Input `yaml:"inputs" json:"Inputs"`
}

type Input struct {
	ID      string `yaml:"id" json:"ID"`
	Type    string `yaml:"type" json:"Type"`
	Min     *int   `yaml:"min,omitempty" json:"Min,omitempty"`
	Max     *int   `yaml:"max,omitempty" json:"Max,omitempty"`
	Default string `yaml:"default,omitempty" json:"Default,omitempty"`
}

type Dependency struct {
	Service string `yaml:"service" json:"Service"`
	Type    string `yaml:"type" json:"Type"` // database / cache / api / queue
}

type SLO struct {
	Availability string `yaml:"availability" json:"Availability"`
	LatencyP99   string `yaml:"latency_p99" json:"LatencyP99"`
}
