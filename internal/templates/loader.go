package templates

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type GoldenPath struct {
	APIVersion string   `yaml:"apiVersion"`
	Kind       string   `yaml:"kind"`
	Metadata   GPMeta   `yaml:"metadata"`
	Spec       GPSpec   `yaml:"spec"`
}

type GPMeta struct {
	Name        string `yaml:"name"`
	Description string `yaml:"description"`
	Icon        string `yaml:"icon"`
}

type GPSpec struct {
	Language      string   `yaml:"language"`
	Type          string   `yaml:"type"`
	CITemplate    string   `yaml:"ci_template"`
	BuildTemplate string   `yaml:"build_template"`
	Includes      []string `yaml:"includes"`
}

type Loader struct {
	BaseURL    string
	Token      string
	RepoPath   string
	client     *http.Client
	goldenPaths []GoldenPath
	ciTemplates map[string]string
}

func NewLoader(baseURL, token, repoPath string) *Loader {
	return &Loader{
		BaseURL:     strings.TrimRight(baseURL, "/"),
		Token:       token,
		RepoPath:    repoPath,
		client:      &http.Client{Timeout: 15 * time.Second},
		ciTemplates: make(map[string]string),
	}
}

func (l *Loader) Load() error {
	if err := l.loadGoldenPaths(); err != nil {
		return fmt.Errorf("golden paths: %w", err)
	}
	if err := l.loadCITemplates(); err != nil {
		return fmt.Errorf("ci templates: %w", err)
	}
	return nil
}

func (l *Loader) GetGoldenPaths() []GoldenPath {
	return l.goldenPaths
}

func (l *Loader) GetCITemplate(path string) (string, bool) {
	content, ok := l.ciTemplates[path]
	return content, ok
}

func (l *Loader) GetCITemplateForLanguage(language string) string {
	key := fmt.Sprintf("ci/%s.gitlab-ci.yml", language)
	if content, ok := l.ciTemplates[key]; ok {
		return content
	}
	return fmt.Sprintf("image: alpine:latest\nstages: [test]\ntest:\n  stage: test\n  script: [echo 'no template for %s']\n", language)
}

func (l *Loader) GetBuildTemplate(imageName string) string {
	if content, ok := l.ciTemplates["ci/build.gitlab-ci.yml"]; ok {
		return strings.ReplaceAll(content, "$IMAGE_NAME", imageName)
	}
	return ""
}

func (l *Loader) listFiles(dir string) ([]string, error) {
	encoded := url.PathEscape(l.RepoPath)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/tree?path=%s&per_page=50",
		l.BaseURL, encoded, url.QueryEscape(dir))

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("PRIVATE-TOKEN", l.Token)
	resp, err := l.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()

	var items []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	json.NewDecoder(resp.Body).Decode(&items)

	var paths []string
	for _, item := range items {
		if item.Type == "blob" {
			paths = append(paths, item.Path)
		}
	}
	return paths, nil
}

func (l *Loader) getFile(filePath string) (string, error) {
	encoded := url.PathEscape(l.RepoPath)
	fileEncoded := url.PathEscape(filePath)
	apiURL := fmt.Sprintf("%s/api/v4/projects/%s/repository/files/%s/raw?ref=main",
		l.BaseURL, encoded, fileEncoded)

	req, _ := http.NewRequest("GET", apiURL, nil)
	req.Header.Set("PRIVATE-TOKEN", l.Token)
	resp, err := l.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	data, err := io.ReadAll(resp.Body)
	return string(data), err
}

func (l *Loader) loadGoldenPaths() error {
	files, err := l.listFiles("golden-paths")
	if err != nil {
		return err
	}

	l.goldenPaths = nil
	for _, f := range files {
		if !strings.HasSuffix(f, ".yaml") {
			continue
		}
		content, err := l.getFile(f)
		if err != nil {
			continue
		}
		var gp GoldenPath
		if err := yaml.Unmarshal([]byte(content), &gp); err != nil {
			continue
		}
		if gp.Kind == "GoldenPath" {
			l.goldenPaths = append(l.goldenPaths, gp)
		}
	}
	return nil
}

func (l *Loader) loadCITemplates() error {
	files, err := l.listFiles("ci")
	if err != nil {
		return err
	}

	for _, f := range files {
		content, err := l.getFile(f)
		if err != nil {
			continue
		}
		l.ciTemplates[f] = content
	}
	return nil
}

// Reload permet de rafraîchir les templates sans redémarrer Symphony
func (l *Loader) Reload() error {
	return l.Load()
}

var _ = os.Getenv // garde l'import
