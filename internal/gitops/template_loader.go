package gitops

import (
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
	"gopkg.in/yaml.v3"
)

// TemplateFile est le nom du fichier qui décrit un golden path dans son dossier
const TemplateFile = "template.yaml"

// GoldenPath est un template de projet chargé depuis le repo de templates.
// Skeleton et CITemplate sont des chemins complets dans le repo Repo, sur la
// branche Branch : ceux d'où il a été chargé, même si la configuration change.
type GoldenPath struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Language    string `json:"language"`
	Type        string `json:"type"`
	SCMProvider string `json:"scm_provider"`
	CIProvider  string `json:"ci_provider"`
	RegProvider string `json:"registry_provider"`
	RTProvider  string `json:"runtime_provider"`
	Repo        string `json:"repo"`
	Branch      string `json:"branch"`
	Skeleton    string `json:"skeleton"`
	CITemplate  string `json:"ci_template"`
}

// templateDoc est le format de <dossier>/template.yaml
type templateDoc struct {
	APIVersion string `yaml:"apiVersion"`
	Kind       string `yaml:"kind"`
	Metadata   struct {
		Name        string `yaml:"name"`
		Title       string `yaml:"title"`
		Description string `yaml:"description"`
		Icon        string `yaml:"icon"`
	} `yaml:"metadata"`
	Spec struct {
		Language  string `yaml:"language"`
		Type      string `yaml:"type"`
		Providers struct {
			SCM      string `yaml:"scm"`
			CI       string `yaml:"ci"`
			Registry string `yaml:"registry"`
			Runtime  string `yaml:"runtime"`
		} `yaml:"providers"`
		Skeleton   string `yaml:"skeleton"`
		CITemplate string `yaml:"ci_template"`
	} `yaml:"spec"`
}

var templateNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]*[a-z0-9])?$`)

// parseTemplate lit le template.yaml du dossier dir (chemin dans le repo).
// Le nom du template doit être celui du dossier, ce qui garantit des id uniques.
func parseTemplate(dir, content string) (GoldenPath, error) {
	var doc templateDoc
	dec := yaml.NewDecoder(strings.NewReader(content))
	dec.KnownFields(true) // une faute de frappe dans une clé est une erreur, pas un champ ignoré
	if err := dec.Decode(&doc); err != nil {
		return GoldenPath{}, fmt.Errorf("parse YAML: %w", err)
	}
	if doc.Kind != "GoldenPath" {
		return GoldenPath{}, fmt.Errorf("kind %q, attendu GoldenPath", doc.Kind)
	}
	name := doc.Metadata.Name
	if !templateNameRe.MatchString(name) {
		return GoldenPath{}, fmt.Errorf("metadata.name %q invalide (minuscules, chiffres, tirets)", name)
	}
	if name != path.Base(dir) {
		return GoldenPath{}, fmt.Errorf("metadata.name %q différent du dossier %q", name, path.Base(dir))
	}
	if doc.Spec.CITemplate == "" {
		return GoldenPath{}, fmt.Errorf("spec.ci_template requis")
	}

	skeleton := doc.Spec.Skeleton
	if skeleton == "" {
		skeleton = "skeleton"
	}
	skeletonPath, err := templatePath(dir, skeleton)
	if err != nil {
		return GoldenPath{}, fmt.Errorf("spec.skeleton: %w", err)
	}
	ciPath, err := templatePath(dir, doc.Spec.CITemplate)
	if err != nil {
		return GoldenPath{}, fmt.Errorf("spec.ci_template: %w", err)
	}

	title := doc.Metadata.Title
	if title == "" {
		title = name
	}
	return GoldenPath{
		ID:          name,
		Name:        title,
		Description: doc.Metadata.Description,
		Icon:        doc.Metadata.Icon,
		Language:    doc.Spec.Language,
		Type:        doc.Spec.Type,
		SCMProvider: doc.Spec.Providers.SCM,
		CIProvider:  doc.Spec.Providers.CI,
		RegProvider: doc.Spec.Providers.Registry,
		RTProvider:  doc.Spec.Providers.Runtime,
		Skeleton:    skeletonPath,
		CITemplate:  ciPath,
	}, nil
}

// templatePath résout un chemin relatif au dossier du template, sans
// permettre d'en sortir (un template ne lit que ses propres fichiers)
func templatePath(dir, rel string) (string, error) {
	if path.IsAbs(rel) {
		return "", fmt.Errorf("chemin %q absolu, attendu relatif au template", rel)
	}
	p := path.Join(dir, rel)
	if p == dir || !strings.HasPrefix(p, dir+"/") {
		return "", fmt.Errorf("chemin %q hors du dossier du template", rel)
	}
	return p, nil
}

// checkProviders vérifie que les providers du golden path existent et que
// son CI tourne sur son SCM. Un provider vide désigne le provider par défaut ;
// le registry est optionnel.
func checkProviders(gp GoldenPath, providers *provider.Registry) error {
	scm, err := providers.SCM(gp.SCMProvider)
	if err != nil {
		return fmt.Errorf("scm provider: %w", err)
	}
	ci, err := providers.CI(gp.CIProvider)
	if err != nil {
		return fmt.Errorf("ci provider: %w", err)
	}
	if err := provider.CheckCompatible(scm, ci); err != nil {
		return err
	}
	if gp.RegProvider != "" {
		if _, err := providers.GetRegistry(gp.RegProvider); err != nil {
			return fmt.Errorf("registry provider: %w", err)
		}
	}
	if _, err := providers.Runtime(gp.RTProvider); err != nil {
		return fmt.Errorf("runtime provider: %w", err)
	}
	return nil
}

// TemplateLoader charge les golden paths depuis le repo de templates
// (<dir>/<nom>/template.yaml) et les recharge périodiquement : ajouter un
// golden path revient à pousser un dossier, sans redémarrer Symphony. Le SCM
// par défaut est résolu à chaque appel, pour suivre les rechargements de
// configuration.
type TemplateLoader struct {
	providers *provider.Registry
	interval  time.Duration

	reloadMu sync.Mutex // un rechargement à la fois, sinon un ancien pourrait écraser un récent

	mu          sync.RWMutex
	source      templateSource
	goldenPaths []GoldenPath
	lastReport  string // pour ne journaliser que les changements
}

type templateSource struct {
	repo   string
	branch string
	dir    string
}

func NewTemplateLoader(providers *provider.Registry, repo, branch, dir string) *TemplateLoader {
	return &TemplateLoader{
		providers: providers,
		source:    templateSource{repo, branch, strings.Trim(dir, "/")},
		interval:  15 * time.Second,
	}
}

// SetSource change le repo de templates (rechargement de configuration) ;
// le rechargement suivant lit le nouveau repo
func (l *TemplateLoader) SetSource(repo, branch, dir string) {
	l.mu.Lock()
	l.source = templateSource{repo, branch, strings.Trim(dir, "/")}
	l.mu.Unlock()
}

func (l *TemplateLoader) Start() {
	l.mu.RLock()
	src := l.source
	l.mu.RUnlock()
	slog.Info("template loader démarré", "repo", src.repo, "branch", src.branch, "path", src.dir, "interval", l.interval)
	l.Reload()
	ticker := time.NewTicker(l.interval)
	for range ticker.C {
		l.Reload()
	}
}

// Reload relit les golden paths. Un template invalide est ignoré, les autres
// sont chargés ; si le repo est illisible, la liste précédente est conservée.
func (l *TemplateLoader) Reload() {
	l.reloadMu.Lock()
	defer l.reloadMu.Unlock()

	l.mu.RLock()
	src := l.source
	l.mu.RUnlock()

	scm, err := l.providers.SCM("")
	if err != nil {
		slog.Warn("templates: provider SCM, liste précédente conservée", "error", err)
		metrics.Reload("templates", "error")
		return
	}
	files, err := scm.ListFiles(src.repo, src.branch, src.dir)
	if err != nil {
		slog.Warn("templates: liste fichiers, liste précédente conservée", "repo", src.repo, "error", err)
		metrics.Reload("templates", "error")
		return
	}

	exists := make(map[string]bool, len(files))
	for _, f := range files {
		exists[f] = true
	}

	var (
		goldenPaths []GoldenPath
		invalid     []string
	)
	for _, filePath := range files {
		// Seulement <dir>/<nom>/template.yaml : pas ceux d'un skeleton
		dir, file := path.Split(filePath)
		dir = strings.TrimSuffix(dir, "/")
		if file != TemplateFile || path.Dir(dir) != src.dirOrDot() {
			continue
		}

		content, err := scm.GetFileContent(src.repo, src.branch, filePath)
		if err != nil {
			slog.Warn("templates: lecture, liste précédente conservée", "path", filePath, "error", err)
			metrics.Reload("templates", "error")
			return
		}
		gp, err := parseTemplate(dir, content)
		gp.Repo, gp.Branch = src.repo, src.branch
		if err == nil {
			err = checkFiles(gp, exists)
		}
		if err == nil {
			err = checkProviders(gp, l.providers)
		}
		if err != nil {
			invalid = append(invalid, fmt.Sprintf("%s: %v", filePath, err))
			continue
		}
		goldenPaths = append(goldenPaths, gp)
	}
	sort.Slice(goldenPaths, func(i, j int) bool { return goldenPaths[i].ID < goldenPaths[j].ID })

	ids := make([]string, len(goldenPaths))
	for i, gp := range goldenPaths {
		ids[i] = gp.ID
	}
	report := strings.Join(append(append([]string(nil), ids...), invalid...), "\n")

	l.mu.Lock()
	l.goldenPaths = goldenPaths
	changed := report != l.lastReport
	l.lastReport = report
	l.mu.Unlock()
	metrics.Reload("templates", "ok")
	metrics.GoldenPaths.Set(float64(len(goldenPaths)))

	// Rechargé toutes les 15 s : on ne journalise que ce qui change
	if changed {
		for _, detail := range invalid {
			slog.Warn("golden path ignoré", "detail", detail)
		}
		slog.Info("golden paths chargés", "count", len(goldenPaths), "ids", ids)
	}
}

// checkFiles vérifie que le skeleton (non vide) et le template CI existent
func checkFiles(gp GoldenPath, exists map[string]bool) error {
	if !exists[gp.CITemplate] {
		return fmt.Errorf("ci_template %s introuvable", gp.CITemplate)
	}
	for f := range exists {
		if strings.HasPrefix(f, gp.Skeleton+"/") {
			return nil
		}
	}
	return fmt.Errorf("skeleton %s introuvable ou vide", gp.Skeleton)
}

// dirOrDot : path.Dir d'un dossier de premier niveau vaut "." quand le
// dossier des templates est la racine du repo
func (s templateSource) dirOrDot() string {
	if s.dir == "" {
		return "."
	}
	return s.dir
}

func (l *TemplateLoader) GetGoldenPaths() []GoldenPath {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]GoldenPath(nil), l.goldenPaths...)
}

func (l *TemplateLoader) GetGoldenPath(id string) (*GoldenPath, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, gp := range l.goldenPaths {
		if gp.ID == id {
			return &gp, nil
		}
	}
	return nil, fmt.Errorf("golden path %q introuvable", id)
}

// GetScaffoldFiles récupère les fichiers du skeleton, avec leur chemin relatif
// au skeleton (= chemin dans le repo créé)
func (l *TemplateLoader) GetScaffoldFiles(gp GoldenPath) (map[string]string, error) {
	scm, err := l.providers.SCM("")
	if err != nil {
		return nil, err
	}
	files, err := scm.ListFiles(gp.Repo, gp.Branch, gp.Skeleton)
	if err != nil {
		return nil, fmt.Errorf("liste skeleton %s: %w", gp.Skeleton, err)
	}

	result := make(map[string]string, len(files))
	for _, filePath := range files {
		content, err := scm.GetFileContent(gp.Repo, gp.Branch, filePath)
		if err != nil {
			return nil, fmt.Errorf("lecture skeleton %s: %w", filePath, err)
		}
		result[strings.TrimPrefix(filePath, gp.Skeleton+"/")] = content
	}
	return result, nil
}

// GetCITemplate récupère le template CI du golden path
func (l *TemplateLoader) GetCITemplate(gp GoldenPath) (string, error) {
	scm, err := l.providers.SCM("")
	if err != nil {
		return "", err
	}
	content, err := scm.GetFileContent(gp.Repo, gp.Branch, gp.CITemplate)
	if err != nil {
		return "", fmt.Errorf("lecture ci_template %s: %w", gp.CITemplate, err)
	}
	return content, nil
}
