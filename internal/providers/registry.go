package providers

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

type IntegrationConfig struct {
	Integrations struct {
		SourceCode struct {
			Provider string         `yaml:"provider"`
			Config   map[string]string `yaml:"config"`
		} `yaml:"source_code"`
		CIAutomation struct {
			Provider string         `yaml:"provider"`
			Config   map[string]string `yaml:"config"`
		} `yaml:"ci_automation"`
		ArtifactStorage struct {
			Provider string         `yaml:"provider"`
			Config   map[string]string `yaml:"config"`
		} `yaml:"artifact_storage"`
		Deployment struct {
			Provider string         `yaml:"provider"`
			Config   map[string]string `yaml:"config"`
		} `yaml:"deployment"`
	} `yaml:"integrations"`
}

func LoadConfig(path string) (*IntegrationConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("lecture config intégrations: %w", err)
	}
	var cfg IntegrationConfig
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config intégrations: %w", err)
	}
	return &cfg, nil
}
