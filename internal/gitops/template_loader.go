package gitops

import (
	"fmt"
	"log/slog"
	"strings"

	"github.com/yourorg/symphony/internal/config"
	"github.com/yourorg/symphony/internal/provider"
)

type TemplateLoader struct {
	scm          provider.SCMProvider
	templateRepo string
	branch       string
	goldenPaths  []config.GoldenPath
}

func NewTemplateLoader(scm provider.SCMProvider, templateRepo, branch string) *TemplateLoader {
	return &TemplateLoader{
		scm:          scm,
		templateRepo: templateRepo,
		branch:       branch,
	}
}

func (l *TemplateLoader) Load(goldenPaths []config.GoldenPath) {
	l.goldenPaths = goldenPaths
	slog.Info("golden paths chargés", "count", len(goldenPaths))
}

func (l *TemplateLoader) GetGoldenPaths() []config.GoldenPath {
	return l.goldenPaths
}

func (l *TemplateLoader) GetGoldenPath(id string) (*config.GoldenPath, error) {
	for _, gp := range l.goldenPaths {
		if gp.ID == id {
			return &gp, nil
		}
	}
	return nil, fmt.Errorf("golden path %q introuvable", id)
}

// GetScaffoldFiles récupère les fichiers de scaffold depuis le repo de templates
func (l *TemplateLoader) GetScaffoldFiles(gp config.GoldenPath) (map[string]string, error) {
	if gp.Scaffold.Source != "git" {
		return nil, fmt.Errorf("source scaffold %q non supportée", gp.Scaffold.Source)
	}

	repo := gp.Scaffold.Repo
	if repo == "" {
		repo = l.templateRepo
	}

	files, err := l.scm.ListFiles(repo, l.branch, gp.Scaffold.Path)
	if err != nil {
		return nil, fmt.Errorf("liste scaffold %s: %w", gp.Scaffold.Path, err)
	}

	result := make(map[string]string, len(files))
	for _, filePath := range files {
		content, err := l.scm.GetFileContent(repo, l.branch, filePath)
		if err != nil {
			slog.Warn("scaffold: lecture fichier", "path", filePath, "error", err)
			continue
		}
		// Chemin relatif dans le repo de destination
		relPath := strings.TrimPrefix(filePath, gp.Scaffold.Path+"/")
		result[relPath] = content
	}
	return result, nil
}

// GetCITemplate récupère le template CI depuis le repo de templates
func (l *TemplateLoader) GetCITemplate(gp config.GoldenPath) (string, error) {
	if gp.CITemplate.Source != "git" {
		return "", fmt.Errorf("source ci_template %q non supportée", gp.CITemplate.Source)
	}

	repo := gp.CITemplate.Repo
	if repo == "" {
		repo = l.templateRepo
	}

	content, err := l.scm.GetFileContent(repo, l.branch, gp.CITemplate.Path)
	if err != nil {
		return "", fmt.Errorf("lecture ci_template %s: %w", gp.CITemplate.Path, err)
	}
	return content, nil
}
