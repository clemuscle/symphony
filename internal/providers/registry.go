package providers

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/joho/godotenv"
	"gopkg.in/yaml.v3"
)

// IntegrationConfig est la source de vérité déclarative des providers.
// Lue depuis config/integrations.yaml au démarrage, avec override par env vars.
type IntegrationConfig struct {
	SCM      SCMConfig      `yaml:"scm"`
	CI       CIConfig       `yaml:"ci"`
	Registry RegistryConfig `yaml:"registry"`
	Deploy   DeployConfig   `yaml:"deploy"`
}

type SCMConfig struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
	// Token n'est jamais lu ni écrit dans le YAML — toujours fourni par
	// variable d'environnement (voir ApplyEnvOverrides). Aucun secret ne
	// peut donc atterrir dans config/integrations.yaml, ni via le wizard
	// ni via une édition manuelle.
	Token string `yaml:"-"`
}

type CIConfig struct {
	Type          string `yaml:"type"`
	ConfigRepo    string `yaml:"config_repo"`
	TemplatesRepo string `yaml:"templates_repo"`
	// Token optionnel, scope minimal dédié CI. Vide => fallback sur
	// SCM.Token (voir BuildProviderSet) — c'est le cas courant tant qu'un
	// seul PAT GitLab couvre SCM+CI+Registry.
	Token string `yaml:"-"`
}

type RegistryConfig struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
	// Token optionnel, scope minimal dédié Registry (ex: GitLab Deploy
	// Token read_registry+write_registry). Vide => fallback sur SCM.Token.
	Token string `yaml:"-"`
}

type DeployConfig struct {
	Type   string `yaml:"type"`
	Socket string `yaml:"socket"`
}

// IsConfigured retourne true si les 4 catégories de provider ont un type
// déclaré. La validité réelle (driver connu, connexion possible) est
// vérifiée par BuildProviderSet, appelé juste après par l'appelant.
func (c *IntegrationConfig) IsConfigured() bool {
	return c.SCM.Type != "" && c.CI.Type != "" && c.Registry.Type != "" && c.Deploy.Type != ""
}

// ApplyEnvOverrides applique les variables d'environnement par-dessus le YAML.
// Les env vars ont priorité — elles permettent d'injecter des secrets sans les
// écrire dans le fichier.
func (c *IntegrationConfig) ApplyEnvOverrides() {
	if v := os.Getenv("GITLAB_URL"); v != "" {
		c.SCM.URL = v
	}
	if v := os.Getenv("GITLAB_TOKEN"); v != "" {
		c.SCM.Token = v
	}
	if v := os.Getenv("SYMPHONY_CI_TOKEN"); v != "" {
		c.CI.Token = v
	}
	if v := os.Getenv("SYMPHONY_REGISTRY_TOKEN"); v != "" {
		c.Registry.Token = v
	}
	if v := os.Getenv("REGISTRY_URL"); v != "" {
		c.Registry.URL = v
	}
	if v := os.Getenv("CONFIG_REPO_PATH"); v != "" {
		c.CI.ConfigRepo = v
	}
	if v := os.Getenv("TEMPLATES_REPO_PATH"); v != "" {
		c.CI.TemplatesRepo = v
	}
	if v := os.Getenv("DOCKER_SOCKET"); v != "" {
		c.Deploy.Socket = v
	}
}

// DefaultEnvPath est le fichier .env géré par Symphony pour les secrets —
// jamais config/integrations.yaml (voir Token yaml:"-" ci-dessus).
const DefaultEnvPath = ".env"

// UpsertEnv fusionne values dans le fichier .env situé à path (le crée si
// absent), sans toucher aux autres clés déjà présentes. Une valeur vide dans
// values est ignorée — c'est la convention "laisser vide = ne pas changer"
// utilisée par le wizard pour ne jamais écraser un secret existant avec un
// champ de formulaire non renseigné.
func UpsertEnv(path string, values map[string]string) error {
	existing, err := godotenv.Read(path)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		existing = map[string]string{}
	}
	changed := false
	for k, v := range values {
		if v == "" {
			continue
		}
		existing[k] = v
		changed = true
	}
	if !changed {
		return nil
	}
	// godotenv.Write crée le fichier via os.Create (0666 masqué par umask,
	// souvent 0644 en pratique) — un fichier de secrets doit rester
	// illisible par les autres utilisateurs de la machine.
	if err := godotenv.Write(existing, path); err != nil {
		return err
	}
	return os.Chmod(path, 0600)
}

const DefaultConfigPath = "config/integrations.yaml"

func LoadConfig(path string) (*IntegrationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg IntegrationConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}
	// Détecte un YAML non vide qui produit une config vide — signe de schéma
	// incorrect (ex: clés inconnues silencieusement ignorées par le parser).
	if len(data) > 0 && cfg.SCM.URL == "" && cfg.CI.ConfigRepo == "" {
		return nil, fmt.Errorf("config: %s parsé mais aucun champ reconnu — vérifier le schéma (voir integrations.example.yaml)", path)
	}
	cfg.ApplyEnvOverrides()
	return &cfg, nil
}

func SaveConfig(path string, cfg *IntegrationConfig) error {
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// ProviderSet regroupe les providers actifs, initialisés depuis IntegrationConfig.
type ProviderSet struct {
	SCM          SCMProvider
	CI           CIProvider
	Registry     RegistryProvider
	Deploy       DeployProvider
	SCMBaseURL   string
	SCMToken     string // token utilisé pour les variables CI (register-service)
	CIConfigRepo string
}
