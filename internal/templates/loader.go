package templates

import (
	"bytes"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"text/template"

	"gopkg.in/yaml.v3"
)

// GoldenPath descriptor — what the API exposes (no file content).
type GoldenPath struct {
	APIVersion string `yaml:"apiVersion" json:"api_version"`
	Kind       string `yaml:"kind"       json:"kind"`
	Metadata   GPMeta `yaml:"metadata"   json:"metadata"`
	Spec       GPSpec `yaml:"spec"       json:"spec"`
}

type GPMeta struct {
	Name        string `yaml:"name"        json:"name"`
	Description string `yaml:"description" json:"description"`
	Icon        string `yaml:"icon"        json:"icon"`
}

type GPSpec struct {
	Language      string `yaml:"language"       json:"language"`
	Type          string `yaml:"type"           json:"type"`
	DefaultPort   int    `yaml:"default_port"   json:"default_port"`
	MonitoringURL string `yaml:"monitoring_url" json:"monitoring_url,omitempty"`
}

// ScaffoldVars are the variables available in golden path template files.
type ScaffoldVars struct {
	ServiceName        string
	ServiceDescription string
	Port               int
	Language           string
	Type               string
	GitServerURL       string
	ConfigRepoPath     string
}

// loadedPath holds a descriptor plus its raw file templates.
type loadedPath struct {
	GoldenPath
	files      map[string]string
	ciPipeline string
}

// Loader loads golden paths from the local filesystem.
type Loader struct {
	baseDir string
	paths   []loadedPath
}

func NewLoader(baseDir string) *Loader {
	return &Loader{baseDir: baseDir}
}

func (l *Loader) Load() error {
	entries, err := os.ReadDir(l.baseDir)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("loader: read dir %s: %w", l.baseDir, err)
	}

	var loaded []loadedPath
	for _, e := range entries {
		if !e.IsDir() {
			continue
		}
		lp, err := l.loadOne(filepath.Join(l.baseDir, e.Name()))
		if err != nil {
			// Un golden path invalide ne casse pas les autres — log + skip
			fmt.Printf("⚠️  golden path %s ignoré: %v\n", e.Name(), err)
			continue
		}
		loaded = append(loaded, lp)
	}
	l.paths = loaded
	return nil
}

func (l *Loader) loadOne(dir string) (loadedPath, error) {
	var lp loadedPath

	data, err := os.ReadFile(filepath.Join(dir, "golden-path.yaml"))
	if err != nil {
		return lp, fmt.Errorf("golden-path.yaml: %w", err)
	}
	if err := yaml.Unmarshal(data, &lp.GoldenPath); err != nil {
		return lp, fmt.Errorf("golden-path.yaml: parse: %w", err)
	}

	filesDir := filepath.Join(dir, "files")
	lp.files = make(map[string]string)
	err = filepath.WalkDir(filesDir, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		rel, _ := filepath.Rel(filesDir, path)
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		lp.files[rel] = string(content)
		return nil
	})
	if err != nil && !os.IsNotExist(err) {
		return lp, fmt.Errorf("files/: %w", err)
	}

	ciData, err := os.ReadFile(filepath.Join(dir, "ci", "pipeline.yml"))
	if err == nil {
		lp.ciPipeline = string(ciData)
	}

	return lp, nil
}

func (l *Loader) Reload() error { return l.Load() }

// GetGoldenPaths returns descriptors only (no file content) — for API listing.
func (l *Loader) GetGoldenPaths() []GoldenPath {
	out := make([]GoldenPath, len(l.paths))
	for i, p := range l.paths {
		out[i] = p.GoldenPath
	}
	return out
}

// RenderFiles renders all golden path template files for the given language.
func (l *Loader) RenderFiles(language string, vars ScaffoldVars) (map[string]string, error) {
	lp, ok := l.getByLanguage(language)
	if !ok {
		return nil, fmt.Errorf("no golden path for language %q", language)
	}
	result := make(map[string]string, len(lp.files))
	for path, raw := range lp.files {
		rendered, err := render(raw, vars)
		if err != nil {
			return nil, fmt.Errorf("render %s: %w", path, err)
		}
		result[path] = rendered
	}
	return result, nil
}

// RenderCI renders the CI pipeline template for the given language.
func (l *Loader) RenderCI(language string, vars ScaffoldVars) (string, error) {
	lp, ok := l.getByLanguage(language)
	if !ok {
		return "", fmt.Errorf("no golden path for language %q", language)
	}
	if lp.ciPipeline == "" {
		return "", nil
	}
	return render(lp.ciPipeline, vars)
}

func (l *Loader) getByLanguage(language string) (loadedPath, bool) {
	for _, p := range l.paths {
		if p.Spec.Language == language {
			return p, true
		}
	}
	return loadedPath{}, false
}

func render(raw string, vars ScaffoldVars) (string, error) {
	tmpl, err := template.New("").Parse(raw)
	if err != nil {
		return "", err
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, vars); err != nil {
		return "", err
	}
	return buf.String(), nil
}
