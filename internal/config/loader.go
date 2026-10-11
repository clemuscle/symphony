package config

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

// Load lit le fichier local : soit un bootstrap (config_source renseigné, la
// configuration est alors à lire dans Git avec Parse), soit une configuration
// complète
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lecture config %s: %w", path, err)
	}
	cfg, err := Parse(data)
	if err != nil {
		return nil, fmt.Errorf("config %s: %w", path, err)
	}
	return cfg, nil
}

// Parse décode une configuration, après expansion des variables d'environnement
func Parse(data []byte) (*Config, error) {
	expanded := os.ExpandEnv(string(data))

	var cfg Config
	if err := yaml.Unmarshal([]byte(expanded), &cfg); err != nil {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}
