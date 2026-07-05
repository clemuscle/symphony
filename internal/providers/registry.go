package providers

import (
	"fmt"
	"os"
	"path/filepath"

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
	Type  string `yaml:"type"`
	URL   string `yaml:"url"`
	Token string `yaml:"token"`
}

type CIConfig struct {
	Type          string `yaml:"type"`
	ConfigRepo    string `yaml:"config_repo"`
	TemplatesRepo string `yaml:"templates_repo"`
}

type RegistryConfig struct {
	Type string `yaml:"type"`
	URL  string `yaml:"url"`
}

type DeployConfig struct {
	Type   string `yaml:"type"`
	Socket string `yaml:"socket"`
}

// IsConfigured retourne true si les champs minimaux requis sont présents.
func (c *IntegrationConfig) IsConfigured() bool {
	return c.SCM.URL != "" && c.SCM.Token != ""
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
	CIConfigRepo string
}
