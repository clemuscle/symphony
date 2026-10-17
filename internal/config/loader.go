package config

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

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

// Parse décode une configuration, après expansion des variables
// d'environnement. Une clé inconnue est une erreur : une faute de frappe ne
// doit pas laisser un réglage de côté sans rien dire.
func Parse(data []byte) (*Config, error) {
	return ParseWith(data, os.Getenv)
}

// ParseWith : Parse, avec les variables lues par getenv
func ParseWith(data []byte, getenv func(string) string) (*Config, error) {
	expanded := os.Expand(string(data), getenv)

	var cfg Config
	dec := yaml.NewDecoder(strings.NewReader(expanded))
	dec.KnownFields(true)
	if err := dec.Decode(&cfg); err != nil && !errors.Is(err, io.EOF) {
		return nil, fmt.Errorf("parse config: %w", err)
	}
	return &cfg, nil
}
