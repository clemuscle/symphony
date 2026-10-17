package config

import "github.com/clemuscle/symphony/internal/environments"

type Config struct {
	// Bootstrap : si renseigné, la configuration (providers, catalogue,
	// stacks) est lue dans un repo Git et le reste du fichier local est ignoré
	ConfigSource *ConfigSource `yaml:"config_source"`

	Providers ProvidersConfig `yaml:"providers"`
	Catalogue CatalogueConfig `yaml:"catalogue"`
	// Stacks proposées à la création d'un projet : repo du client et
	// catalogue officiel
	Stacks *StacksConfig `yaml:"stacks"`
	// Templates : ancien nom de Stacks (golden paths), lu jusqu'à la v1.0
	Templates *TemplatesConfig `yaml:"templates"`

	// Auth absent : authentification désactivée (tout le monde est admin)
	Auth *AuthConfig `yaml:"auth"`
	// Origines autorisées à appeler l'API depuis un autre site (CORS). Vide :
	// même origine seulement, le front étant servi par Symphony.
	CORSOrigins []string `yaml:"cors_origins"`

	// Environnements de déploiement, dans l'ordre de promotion (staging, puis
	// production…) ; absent : un déploiement par projet, sans environnement
	Environments []environments.Environment `yaml:"environments"`

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

// StacksConfig : repo Git des stacks du client, un dossier par stack
// (<path>/<nom>/stack.yaml), lu via le provider SCM par défaut ; vide : pas
// de repo. Official : stacks du catalogue officiel proposées ("*" : toutes).
type StacksConfig struct {
	Repo     string   `yaml:"repo"`
	Branch   string   `yaml:"branch"`
	Path     string   `yaml:"path"`
	Official []string `yaml:"official"`
}

// TemplatesConfig : l'ancienne section templates (repo des golden paths)
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
