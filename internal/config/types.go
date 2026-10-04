package config

type Config struct {
	Providers  ProvidersConfig   `yaml:"providers"`
	GoldenPaths []GoldenPath     `yaml:"golden_paths"`
	Catalogue  CatalogueConfig   `yaml:"catalogue"`
}

type ProvidersConfig struct {
	SCM      []ProviderDef `yaml:"scm"`
	CI       []ProviderDef `yaml:"ci"`
	Registry []ProviderDef `yaml:"registry"`
	Runtime  []ProviderDef `yaml:"runtime"`
}

type ProviderDef struct {
	ID       string            `yaml:"id"`
	Type     string            `yaml:"type"`
	Default  bool              `yaml:"default"`
	Config   map[string]string `yaml:"config"`
}

type GoldenPath struct {
	ID          string      `yaml:"id" json:"id"`
	Name        string      `yaml:"name" json:"name"`
	Description string      `yaml:"description" json:"description"`
	Icon        string      `yaml:"icon" json:"icon"`
	Language    string      `yaml:"language" json:"language"`
	Type        string      `yaml:"type" json:"type"`
	SCMProvider string      `yaml:"scm_provider" json:"scm_provider"`
	CIProvider  string      `yaml:"ci_provider" json:"ci_provider"`
	RegProvider string      `yaml:"registry_provider" json:"registry_provider"`
	RTProvider  string      `yaml:"runtime_provider" json:"runtime_provider"`
	Scaffold    ScaffoldRef `yaml:"scaffold" json:"scaffold"`
	CITemplate  FileRef     `yaml:"ci_template" json:"ci_template"`
}

type ScaffoldRef struct {
	Source string `yaml:"source" json:"source"` // "git" ou "inline"
	Repo   string `yaml:"repo" json:"repo"`
	Path   string `yaml:"path" json:"path"`
}

type FileRef struct {
	Source string `yaml:"source" json:"source"`
	Repo   string `yaml:"repo" json:"repo"`
	Path   string `yaml:"path" json:"path"`
}

type CatalogueConfig struct {
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
	Path   string `yaml:"path"`
}
