package costs

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Currency string `yaml:"currency"`
	Rates    Rates  `yaml:"rates"`
}

type Rates struct {
	// ContainerHourly is the cost per container-hour (prod deployments + recettes).
	ContainerHourly float64 `yaml:"container_hourly"`
	// CIMinute is the cost per pipeline-minute consumed.
	CIMinute float64 `yaml:"ci_minute"`
}

func DefaultConfig() Config {
	return Config{Currency: "EUR"}
}

func LoadConfig(path string) (Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return DefaultConfig(), err
	}
	cfg := DefaultConfig()
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return DefaultConfig(), err
	}
	if cfg.Currency == "" {
		cfg.Currency = "EUR"
	}
	return cfg, nil
}
