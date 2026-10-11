package config

type Config struct {
	// Bootstrap : si renseigné, la configuration (providers, catalogue,
	// templates) est lue dans un repo Git et le reste du fichier local est ignoré
	ConfigSource *ConfigSource `yaml:"config_source"`

	Providers ProvidersConfig `yaml:"providers"`
	Catalogue CatalogueConfig `yaml:"catalogue"`
	Templates TemplatesConfig `yaml:"templates"`
}

type ProvidersConfig struct {
	SCM      []ProviderDef `yaml:"scm"`
	CI       []ProviderDef `yaml:"ci"`
	Registry []ProviderDef `yaml:"registry"`
	Runtime  []ProviderDef `yaml:"runtime"`
}

type ProviderDef struct {
	ID      string            `yaml:"id"`
	Type    string            `yaml:"type"`
	Default bool              `yaml:"default"`
	Config  map[string]string `yaml:"config"`
}

type CatalogueConfig struct {
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
	Path   string `yaml:"path"`
}

// TemplatesConfig : repo Git des golden paths, un dossier par template
// (<path>/<nom>/template.yaml), lu via le provider SCM par défaut
type TemplatesConfig struct {
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
	Path   string `yaml:"path"`
}

// ConfigSource : où lire la configuration dans Git, et le SCM à utiliser pour
// la lire (même format qu'un provider SCM ; il n'est pas enregistré)
type ConfigSource struct {
	SCM    ProviderDef `yaml:"scm"`
	Repo   string      `yaml:"repo"`
	Branch string      `yaml:"branch"`
	Path   string      `yaml:"path"`
}
