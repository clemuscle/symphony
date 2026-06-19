package catalog

import (
	"fmt"
	"os"
	"path/filepath"

	"gopkg.in/yaml.v3"
)

// ParseDirectory scanne un dossier et parse tous les .yaml trouvés
func ParseDirectory(root string) ([]Service, error) {
	var services []Service

	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		if filepath.Ext(path) != ".yaml" && filepath.Ext(path) != ".yml" {
			return nil
		}

		data, err := os.ReadFile(path)
		if err != nil {
			return fmt.Errorf("lecture %s: %w", path, err)
		}

		var svc Service
		if err := yaml.Unmarshal(data, &svc); err != nil {
			return fmt.Errorf("parse %s: %w", path, err)
		}

		// On ne traite que les objets de type Service pour l'instant
		if svc.Kind == "Service" {
			services = append(services, svc)
		}
		return nil
	})

	return services, err
}

// ParseFile parse un seul fichier YAML
func ParseFile(path string) (*Service, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var svc Service
	if err := yaml.Unmarshal(data, &svc); err != nil {
		return nil, err
	}
	return &svc, nil
}