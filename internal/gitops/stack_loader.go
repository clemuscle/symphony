package gitops

import (
	"fmt"
	"io/fs"
	"log/slog"
	"path"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
)

// StackSource : d'où viennent les stacks proposées. Repo (vide : aucun) est
// le repo de stacks du client, lu avec le SCM par défaut, un dossier par
// stack sous Path ; Official les stacks du catalogue officiel exposées ("*" :
// toutes).
type StackSource struct {
	Repo     string
	Branch   string
	Path     string
	Official []string
}

// StackLoader charge les stacks et les recharge périodiquement : ajouter une
// stack revient à pousser un dossier dans le repo, sans redémarrer Symphony.
// Le SCM par défaut est résolu à chaque appel, pour suivre les rechargements
// de configuration.
type StackLoader struct {
	providers *provider.Registry
	official  fs.FS // catalogue officiel : <nom>/stack.yaml
	interval  time.Duration

	reloadMu sync.Mutex // un rechargement à la fois, sinon un ancien pourrait écraser un récent

	mu         sync.RWMutex
	source     StackSource
	stacks     []Stack
	lastReport string // pour ne journaliser que les changements
	report     StackReport
}

// StackReport : le dernier rechargement des stacks, pour la page
// Administration
type StackReport struct {
	Repo     string   `json:"repo"`
	Branch   string   `json:"branch"`
	Path     string   `json:"path"`
	Official []string `json:"official"`
	// LoadedAt : dernier rechargement réussi (repo lu, même si des stacks y
	// sont refusées)
	LoadedAt time.Time `json:"loaded_at"`
	// Error : le dernier rechargement a échoué (repo illisible…) ; la liste
	// précédente reste en place
	Error   string         `json:"error,omitempty"`
	Refused []RefusedStack `json:"refused"`
	// Warnings : stacks chargées, mais à corriger (ancien format, stack
	// officielle remplacée par celle du repo)
	Warnings []string `json:"warnings"`
}

// RefusedStack : une stack refusée, et pourquoi
type RefusedStack struct {
	File  string `json:"file"` // <dossier>/<nom>/stack.yaml, ou officiel:<nom>
	Error string `json:"error"`
}

// Report : le dernier rechargement
func (l *StackLoader) Report() StackReport {
	l.mu.RLock()
	defer l.mu.RUnlock()
	r := l.report
	r.Repo, r.Branch, r.Path = l.source.Repo, l.source.Branch, l.source.Path
	r.Official = append([]string{}, l.source.Official...)
	r.Refused = append([]RefusedStack{}, r.Refused...)
	r.Warnings = append([]string{}, r.Warnings...)
	return r
}

// failed note l'échec d'un rechargement
func (l *StackLoader) failed(err error) {
	l.mu.Lock()
	l.report.Error = err.Error()
	l.mu.Unlock()
	metrics.Reload("stacks", "error")
}

// NewStackLoader : official est le catalogue officiel (stacks.FS en
// production, un fstest.MapFS dans les tests)
func NewStackLoader(providers *provider.Registry, official fs.FS, src StackSource) *StackLoader {
	return &StackLoader{
		providers: providers,
		official:  official,
		source:    normalizeSource(src),
		interval:  15 * time.Second,
	}
}

func normalizeSource(src StackSource) StackSource {
	src.Path = strings.Trim(src.Path, "/")
	src.Official = append([]string(nil), src.Official...)
	return src
}

// SetSource change les sources (rechargement de configuration) ; le
// rechargement suivant les lit
func (l *StackLoader) SetSource(src StackSource) {
	l.mu.Lock()
	l.source = normalizeSource(src)
	l.mu.Unlock()
}

func (l *StackLoader) Start() {
	l.mu.RLock()
	src := l.source
	l.mu.RUnlock()
	slog.Info("stacks : chargement", "repo", src.Repo, "branch", src.Branch, "path", src.Path,
		"official", src.Official, "interval", l.interval)
	l.Reload()
	ticker := time.NewTicker(l.interval)
	for range ticker.C {
		l.Reload()
	}
}

// Reload relit les stacks. Une stack invalide est refusée seule, les autres
// sont chargées ; si le repo est illisible, la liste précédente est conservée.
func (l *StackLoader) Reload() {
	l.reloadMu.Lock()
	defer l.reloadMu.Unlock()

	l.mu.RLock()
	src := l.source
	l.mu.RUnlock()

	var (
		loaded   []Stack
		refused  []RefusedStack
		warnings []string
	)
	official, offRefused := l.loadOfficial(src.Official)
	refused = append(refused, offRefused...)

	if src.Repo != "" {
		repoStacks, repoRefused, repoWarnings, err := l.loadRepo(src)
		if err != nil {
			slog.Warn("stacks : repo illisible, liste précédente conservée", "repo", src.Repo, "error", err)
			l.failed(err)
			return
		}
		loaded, refused, warnings = repoStacks, append(refused, repoRefused...), repoWarnings
	}
	// Une stack du repo remplace la stack officielle du même nom
	ids := map[string]bool{}
	for _, s := range loaded {
		ids[s.ID] = true
	}
	for _, s := range official {
		if ids[s.ID] {
			warnings = append(warnings, fmt.Sprintf("%s : la stack du repo remplace la stack officielle du même nom", s.ID))
			continue
		}
		loaded = append(loaded, s)
	}
	sort.Slice(loaded, func(i, j int) bool { return loaded[i].ID < loaded[j].ID })
	sort.Strings(warnings)

	names := make([]string, len(loaded))
	for i, s := range loaded {
		names[i] = s.ID
	}
	var lines []string
	for _, r := range refused {
		lines = append(lines, r.File+": "+r.Error)
	}
	summary := strings.Join(append(append(append([]string(nil), names...), lines...), warnings...), "\n")

	l.mu.Lock()
	l.stacks = loaded
	l.report = StackReport{LoadedAt: time.Now(), Refused: refused, Warnings: warnings}
	changed := summary != l.lastReport
	l.lastReport = summary
	l.mu.Unlock()
	metrics.Reload("stacks", "ok")
	metrics.Stacks.Set(float64(len(loaded)))

	// Rechargé toutes les 15 s : on ne journalise que ce qui change
	if changed {
		for _, line := range lines {
			slog.Warn("stack refusée", "detail", line)
		}
		for _, w := range warnings {
			slog.Warn("stack à corriger", "detail", w)
		}
		slog.Info("stacks chargées", "count", len(loaded), "ids", names)
	}
}

// loadRepo lit les stacks du repo du client : <path>/<nom>/stack.yaml, ou
// l'ancien template.yaml
func (l *StackLoader) loadRepo(src StackSource) (stacks []Stack, refused []RefusedStack, warnings []string, err error) {
	scm, err := l.providers.SCM("")
	if err != nil {
		return nil, nil, nil, err
	}
	files, err := scm.ListFiles(src.Repo, src.Branch, src.Path)
	if err != nil {
		return nil, nil, nil, fmt.Errorf("lecture de %s (branche %s, dossier %s) : %w", src.Repo, src.Branch, dirOrDot(src.Path), err)
	}
	exists := make(map[string]bool, len(files))
	for _, f := range files {
		exists[f] = true
	}
	for _, dir := range stackDirs(exists, dirOrDot(src.Path)) {
		file := StackFile
		if !exists[path.Join(dir, StackFile)] {
			file = LegacyStackFile
		}
		filePath := path.Join(dir, file)
		content, err := scm.GetFileContent(src.Repo, src.Branch, filePath)
		if err != nil {
			return nil, nil, nil, fmt.Errorf("lecture de %s : %w", filePath, err)
		}
		s, err := CheckStack(dir, file, content, exists, l.providers)
		if err != nil {
			refused = append(refused, RefusedStack{File: filePath, Error: err.Error()})
			continue
		}
		s.Repo, s.Branch = src.Repo, src.Branch
		if s.Legacy {
			warnings = append(warnings, fmt.Sprintf("%s : ancien format (template.yaml, kind GoldenPath), renommer en stack.yaml avec kind Stack", filePath))
		}
		if file == StackFile && exists[path.Join(dir, LegacyStackFile)] {
			warnings = append(warnings, fmt.Sprintf("%s : template.yaml ignoré, stack.yaml est lu", dir))
		}
		stacks = append(stacks, s)
	}
	return stacks, refused, warnings, nil
}

// stackDirs : les dossiers <parent>/<nom> qui ont un stack.yaml ou un
// template.yaml (pas ceux d'un squelette, plus profonds)
func stackDirs(exists map[string]bool, parent string) []string {
	seen := map[string]bool{}
	var dirs []string
	for f := range exists {
		dir, file := path.Split(f)
		dir = strings.TrimSuffix(dir, "/")
		if (file != StackFile && file != LegacyStackFile) || path.Dir(dir) != parent || dir == "" || seen[dir] {
			continue
		}
		seen[dir] = true
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

// loadOfficial lit les stacks officielles exposées ("*" : toutes)
func (l *StackLoader) loadOfficial(names []string) ([]Stack, []RefusedStack) {
	if len(names) == 0 || l.official == nil {
		return nil, nil
	}
	exists := map[string]bool{}
	fs.WalkDir(l.official, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			exists[p] = true
		}
		return nil
	})
	available := stackDirs(exists, ".")
	if contains(names, "*") {
		names = available
	}
	var (
		stacks  []Stack
		refused []RefusedStack
	)
	for _, name := range names {
		file := "officiel:" + name
		if !contains(available, name) {
			refused = append(refused, RefusedStack{File: file, Error: "stack officielle inconnue (" + strings.Join(available, ", ") + ")"})
			continue
		}
		content, err := fs.ReadFile(l.official, path.Join(name, StackFile))
		if err != nil {
			refused = append(refused, RefusedStack{File: file, Error: err.Error()})
			continue
		}
		s, err := CheckStack(name, StackFile, string(content), exists, l.providers)
		if err != nil {
			refused = append(refused, RefusedStack{File: file, Error: err.Error()})
			continue
		}
		s.Official = true
		stacks = append(stacks, s)
	}
	return stacks, refused
}

// OfficialNames : les stacks du catalogue officiel official
func OfficialNames(official fs.FS) []string {
	exists := map[string]bool{}
	fs.WalkDir(official, ".", func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			exists[p] = true
		}
		return nil
	})
	return stackDirs(exists, ".")
}

// dirOrDot : path.Dir d'un dossier de premier niveau vaut "." quand les
// stacks sont à la racine du repo
func dirOrDot(dir string) string {
	if dir == "" {
		return "."
	}
	return dir
}

func (l *StackLoader) GetStacks() []Stack {
	l.mu.RLock()
	defer l.mu.RUnlock()
	return append([]Stack(nil), l.stacks...)
}

func (l *StackLoader) GetStack(id string) (*Stack, error) {
	l.mu.RLock()
	defer l.mu.RUnlock()
	for _, s := range l.stacks {
		if s.ID == id {
			return &s, nil
		}
	}
	return nil, fmt.Errorf("stack %q introuvable", id)
}

// listFiles et readFile lisent dans la source de la stack : le catalogue
// officiel, ou son repo (avec le SCM par défaut)
func (l *StackLoader) listFiles(s Stack, dir string) ([]string, error) {
	if s.Official {
		var files []string
		err := fs.WalkDir(l.official, dir, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() {
				files = append(files, p)
			}
			return err
		})
		return files, err
	}
	scm, err := l.providers.SCM("")
	if err != nil {
		return nil, err
	}
	return scm.ListFiles(s.Repo, s.Branch, dir)
}

func (l *StackLoader) readFile(s Stack, p string) (string, error) {
	if s.Official {
		b, err := fs.ReadFile(l.official, p)
		return string(b), err
	}
	scm, err := l.providers.SCM("")
	if err != nil {
		return "", err
	}
	return scm.GetFileContent(s.Repo, s.Branch, p)
}

// TemplateSuffix : un fichier du squelette qui porte ce suffixe est créé sans
// lui (go.mod.tmpl → go.mod) : les outils du repo de stacks ne le prennent
// pas pour le leur (module Go, CI, workflow GitHub)
const TemplateSuffix = ".tmpl"

// GetScaffoldFiles récupère les fichiers du squelette, avec leur chemin
// dans le repo créé (relatif au squelette, sans le suffixe .tmpl)
func (l *StackLoader) GetScaffoldFiles(s Stack) (map[string]string, error) {
	files, err := l.listFiles(s, s.Skeleton)
	if err != nil {
		return nil, fmt.Errorf("liste skeleton %s: %w", s.Skeleton, err)
	}
	result := make(map[string]string, len(files))
	for _, filePath := range files {
		content, err := l.readFile(s, filePath)
		if err != nil {
			return nil, fmt.Errorf("lecture skeleton %s: %w", filePath, err)
		}
		result[RepoPath(s, filePath)] = content
	}
	return result, nil
}

// RepoPath : le chemin dans le repo créé d'un fichier du squelette
func RepoPath(s Stack, file string) string {
	return strings.TrimSuffix(strings.TrimPrefix(file, s.Skeleton+"/"), TemplateSuffix)
}

// GetCITemplate récupère la CI de la stack (pour la plateforme de son CI)
func (l *StackLoader) GetCITemplate(s Stack) (string, error) {
	content, err := l.readFile(s, s.CITemplate)
	if err != nil {
		return "", fmt.Errorf("lecture ci_template %s: %w", s.CITemplate, err)
	}
	return content, nil
}
