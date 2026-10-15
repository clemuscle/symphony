package config

type Config struct {
	// Bootstrap : si renseigné, la configuration (providers, catalogue,
	// templates) est lue dans un repo Git et le reste du fichier local est ignoré
	ConfigSource *ConfigSource `yaml:"config_source"`

	Providers ProvidersConfig `yaml:"providers"`
	Catalogue CatalogueConfig `yaml:"catalogue"`
	Templates TemplatesConfig `yaml:"templates"`

	// Auth absent : authentification désactivée (tout le monde est admin)
	Auth *AuthConfig `yaml:"auth"`
	// Origines autorisées à appeler l'API depuis un autre site (CORS). Vide :
	// même origine seulement, le front étant servi par Symphony.
	CORSOrigins []string `yaml:"cors_origins"`

	// Webhooks entrants (événements de pipeline) ; absent : les tâches suivent
	// les pipelines par sondage seulement
	Webhooks *WebhooksConfig `yaml:"webhooks"`
}

// WebhooksConfig : Symphony abonne les repos qu'il crée à leurs événements de
// pipeline, reçus sur <url>/webhooks/gitlab
type WebhooksConfig struct {
	URL string `yaml:"url"` // Symphony vu par GitLab, ex. http://symphony:8080
	// Secret partagé (16 caractères min.) : référence secret://, ex.
	// secret://env/WEBHOOK_SECRET
	Secret string `yaml:"secret"`
}

// AuthConfig : provider d'identité de la connexion et correspondance
// groupes → rôles
type AuthConfig struct {
	// Provider identity de la connexion ; vide : celui par défaut
	Identity string `yaml:"identity"`
	// Rôle (viewer, developer, admin) → groupes OIDC qui le donnent
	Roles map[string][]string `yaml:"roles"`
}

type ProvidersConfig struct {
	SCM      []ProviderDef `yaml:"scm"`
	CI       []ProviderDef `yaml:"ci"`
	Registry []ProviderDef `yaml:"registry"`
	Runtime  []ProviderDef `yaml:"runtime"`
	// Secrets : lus avant les autres, qui y font référence (secret://<id>/…).
	// Le provider "env" (variables d'environnement) existe toujours.
	Secrets  []ProviderDef `yaml:"secrets"`
	Identity []ProviderDef `yaml:"identity"`
}

// ProviderDef : un provider. Toute valeur de Config peut être une référence
// secret://<provider>/<chemin>#<clé>, remplacée par le secret avant usage.
type ProviderDef struct {
	ID      string            `yaml:"id"`
	Type    string            `yaml:"type"`
	Default bool              `yaml:"default"`
	Config  map[string]string `yaml:"config"`
}

// CatalogueConfig : chaque projet porte sa fiche dans son repo (File, défaut
// catalog-info.yaml)
type CatalogueConfig struct {
	File string `yaml:"file"`
}

// TemplatesConfig : repo Git des golden paths, un dossier par template
// (<path>/<nom>/template.yaml), lu via le provider SCM par défaut
type TemplatesConfig struct {
	Repo   string `yaml:"repo"`
	Branch string `yaml:"branch"`
	Path   string `yaml:"path"`
}

// ConfigSource : où lire la configuration dans Git, et le SCM à utiliser pour
// la lire (même format qu'un provider SCM ; il n'est pas enregistré). Son
// jeton peut venir des providers.secrets du bootstrap.
type ConfigSource struct {
	SCM    ProviderDef `yaml:"scm"`
	Repo   string      `yaml:"repo"`
	Branch string      `yaml:"branch"`
	Path   string      `yaml:"path"`
}
