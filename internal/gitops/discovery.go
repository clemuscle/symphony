package gitops

import (
	"errors"
	"fmt"
	"log/slog"
	"path"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
)

// Découverte des services existants : chaque repo d'un groupe GitLab ou d'une
// organisation GitHub qui porte une fiche (catalog-info.yaml) devient un
// projet de Symphony, sans rien écrire dans le repo. Son nom est celui de la
// fiche, son équipe son spec.owner.

// DefaultDiscoveryInterval : délai entre deux scans
const DefaultDiscoveryInterval = 5 * time.Minute

// ProjectNameRe : nom de projet (DNS : utilisable tel quel comme nom
// Kubernetes, d'image et d'hôte)
var ProjectNameRe = regexp.MustCompile(`^[a-z0-9]([a-z0-9-]{0,61}[a-z0-9])?$`)

// DiscoverySource : un groupe scanné, providers résolus
type DiscoverySource struct {
	SCM      string   `json:"scm"`
	Group    string   `json:"group"`
	CI       string   `json:"ci"`
	Registry string   `json:"registry"`
	Exclude  []string `json:"exclude,omitempty"`
}

// Key : identifiant de la source, enregistré avec ses projets
func (s DiscoverySource) Key() string { return s.SCM + "/" + s.Group }

// excluded : le repo correspond à un motif d'exclusion
func (s DiscoverySource) excluded(repoPath string) bool {
	for _, pattern := range s.Exclude {
		if ok, _ := path.Match(pattern, repoPath); ok {
			return true
		}
	}
	return false
}

// DiscoverySettings : la section catalogue.discovery, validée
type DiscoverySettings struct {
	Interval time.Duration
	Sources  []DiscoverySource
}

// KnownProject : un projet en base, vu par la découverte
type KnownProject struct {
	Name           string
	RepoPath       string
	SCMProvider    string
	DiscoveredFrom string
}

// FoundProject : un projet trouvé par la découverte, à enregistrer ou à
// mettre à jour
type FoundProject struct {
	Name        string
	Description string
	Language    string
	Type        string
	Port        int
	Team        string
	Source      DiscoverySource
	Repo        provider.Repo
}

// DiscoveryStore : les projets de Symphony (la base)
type DiscoveryStore interface {
	KnownProjects() ([]KnownProject, error)
	AddProject(p FoundProject) error
	UpdateProject(p FoundProject) error
}

// DiscoveryReport : le dernier scan, pour la page Administration
type DiscoveryReport struct {
	Enabled  bool              `json:"enabled"`
	Interval string            `json:"interval"`
	ScanAt   *time.Time        `json:"scan_at"`
	Sources  []SourceReport    `json:"sources"`
	Errors   []DiscoveryError  `json:"errors"`
	Orphans  []DiscoveryOrphan `json:"orphans"`
	// Error : la liste des projets est illisible (base de données)
	Error string `json:"error,omitempty"`
}

// SourceReport : ce qu'une source a donné
type SourceReport struct {
	DiscoverySource
	Key   string `json:"key"`
	Repos int    `json:"repos"` // repos parcourus (hors exclus)
	// Projects : projets découverts dans cette source
	Projects []string `json:"projects"`
	// Imported : nouveaux à ce scan
	Imported []string `json:"imported"`
	Excluded int      `json:"excluded"`
	NoFiche  int      `json:"no_fiche"` // repos sans fiche
	Error    string   `json:"error,omitempty"`
}

// DiscoveryError : un repo avec une fiche inutilisable, ou en conflit
type DiscoveryError struct {
	Source string `json:"source"`
	Repo   string `json:"repo"`
	Error  string `json:"error"`
}

// DiscoveryOrphan : un projet découvert que sa source ne donne plus (fiche
// retirée, repo exclu, supprimé ou archivé, source retirée de la
// configuration). Il reste dans Symphony : un admin le retire.
type DiscoveryOrphan struct {
	Project string `json:"project"`
	Source  string `json:"source"`
	Repo    string `json:"repo"`
}

// Discoverer scanne les sources de la configuration. Les providers sont
// résolus à chaque scan : il suit les rechargements de configuration.
type Discoverer struct {
	providers *provider.Registry
	store     DiscoveryStore
	file      func() string // nom de la fiche (catalogue.file)
	onChange  func()        // un projet ajouté : le catalogue relit les fiches

	// Teams : les équipes de l'annuaire, pour vérifier le spec.owner des
	// fiches ; nil (ou une réponse nil : pas d'annuaire) : non vérifiées
	Teams func() map[string]bool

	scanMu sync.Mutex
	wake   chan struct{}

	mu       sync.RWMutex
	settings DiscoverySettings
	report   DiscoveryReport
	matched  map[string]bool // projets que leur source donne encore
}

func NewDiscoverer(providers *provider.Registry, store DiscoveryStore, file func() string, onChange func()) *Discoverer {
	return &Discoverer{
		providers: providers,
		store:     store,
		file:      file,
		onChange:  onChange,
		wake:      make(chan struct{}, 1),
		matched:   map[string]bool{},
	}
}

// SetSettings change les sources (rechargement de configuration) et lance un
// scan si elles ont changé
func (d *Discoverer) SetSettings(s DiscoverySettings) {
	d.mu.Lock()
	changed := !slices.EqualFunc(d.settings.Sources, s.Sources, func(a, b DiscoverySource) bool {
		return a.Key() == b.Key() && a.CI == b.CI && a.Registry == b.Registry && slices.Equal(a.Exclude, b.Exclude)
	}) || d.settings.Interval != s.Interval
	d.settings = s
	d.mu.Unlock()
	if changed {
		d.Wake()
	}
}

// Wake demande un scan sans attendre l'intervalle
func (d *Discoverer) Wake() {
	if d == nil {
		return
	}
	select {
	case d.wake <- struct{}{}:
	default:
	}
}

// Start scanne à intervalle régulier, ou quand Wake est appelé
func (d *Discoverer) Start() {
	for {
		select { // un Wake d'avant ce scan est servi par lui
		case <-d.wake:
		default:
		}
		d.Scan()
		d.mu.RLock()
		interval := d.settings.Interval
		d.mu.RUnlock()
		if interval <= 0 {
			interval = DefaultDiscoveryInterval
		}
		select {
		case <-d.wake:
		case <-time.After(interval):
		}
	}
}

// Report : le dernier scan
func (d *Discoverer) Report() DiscoveryReport {
	if d == nil {
		return DiscoveryReport{Sources: []SourceReport{}, Errors: []DiscoveryError{}, Orphans: []DiscoveryOrphan{}}
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	r := d.report
	r.Enabled = len(d.settings.Sources) > 0
	r.Interval = FormatInterval(cmpInterval(d.settings.Interval))
	r.Sources = slices.Clone(r.Sources)
	r.Errors = slices.Clone(r.Errors)
	r.Orphans = slices.Clone(r.Orphans)
	return r
}

// FormatInterval : 10m plutôt que 10m0s
func FormatInterval(d time.Duration) string {
	s := d.String()
	if strings.HasSuffix(s, "m0s") {
		s = strings.TrimSuffix(s, "0s")
	}
	if strings.HasSuffix(s, "h0m") {
		s = strings.TrimSuffix(s, "0m")
	}
	return s
}

func cmpInterval(i time.Duration) time.Duration {
	if i <= 0 {
		return DefaultDiscoveryInterval
	}
	return i
}

// Matched : le projet découvert name est encore donné par sa source (le
// retirer de Symphony ne servirait à rien : le scan suivant le reprendrait)
func (d *Discoverer) Matched(name string) bool {
	if d == nil {
		return false
	}
	d.mu.RLock()
	defer d.mu.RUnlock()
	return d.matched[name]
}

// Scan parcourt chaque source une fois
func (d *Discoverer) Scan() {
	d.scanMu.Lock()
	defer d.scanMu.Unlock()

	d.mu.RLock()
	settings := d.settings
	d.mu.RUnlock()
	if len(settings.Sources) == 0 {
		d.mu.Lock()
		d.report = DiscoveryReport{}
		d.matched = map[string]bool{}
		d.mu.Unlock()
		return
	}

	now := time.Now()
	report := DiscoveryReport{ScanAt: &now}
	known, err := d.store.KnownProjects()
	if err != nil {
		slog.Warn("découverte : projets illisibles", "error", err)
		metrics.Reload("discovery", "error")
		d.mu.Lock()
		d.report.Error = err.Error()
		d.mu.Unlock()
		return
	}
	byName := map[string]KnownProject{}
	byRepo := map[string]KnownProject{} // scm/repo → projet
	for _, k := range known {
		byName[k.Name] = k
		byRepo[k.SCMProvider+"/"+k.RepoPath] = k
	}

	file := d.file()
	var teams map[string]bool
	if d.Teams != nil {
		teams = d.Teams()
	}
	matched := map[string]bool{}
	scanned := map[string]bool{} // sources lues sans erreur
	added := false
	failed := false
	for _, src := range settings.Sources {
		sr := SourceReport{DiscoverySource: src, Key: src.Key(), Projects: []string{}, Imported: []string{}}
		fail := func(repo string, err error) {
			report.Errors = append(report.Errors, DiscoveryError{Source: src.Key(), Repo: repo, Error: err.Error()})
		}
		scm, err := d.providers.SCM(src.SCM)
		var repos []provider.Repo
		if err == nil {
			repos, err = scm.ListGroupRepos(src.Group)
		}
		if err != nil {
			slog.Warn("découverte : groupe illisible", "source", src.Key(), "error", err)
			sr.Error = err.Error()
			failed = true
			report.Sources = append(report.Sources, sr)
			continue
		}
		scanned[src.Key()] = true
		for _, repo := range repos {
			if src.excluded(repo.Path) {
				sr.Excluded++
				continue
			}
			sr.Repos++
			content, err := scm.GetFileContent(repo.Path, repo.DefaultBranch, file)
			if errors.Is(err, provider.ErrNotFound) {
				sr.NoFiche++
				continue
			}
			if err != nil {
				// Repo illisible : son projet, s'il existe, n'est pas orphelin
				if k, ok := byRepo[src.SCM+"/"+repo.Path]; ok {
					matched[k.Name] = true
				}
				fail(repo.Path, err)
				failed = true
				continue
			}
			found, err := foundProject(content, src, repo)
			if err != nil {
				fail(repo.Path, err)
				continue
			}
			// Une équipe inconnue ne donne de droits à personne : le projet
			// est réservé aux admins, et l'erreur se voit (vérifié une fois
			// le projet retenu : un conflit de nom suffit à dire)
			checkTeam := func() {
				if teams != nil && !teams[found.Team] {
					fail(repo.Path, fmt.Errorf("équipe %q (spec.owner) absente de l'annuaire : projet réservé aux admins", found.Team))
					found.Team = ""
				}
			}
			if k, ok := byRepo[src.SCM+"/"+repo.Path]; ok {
				if k.DiscoveredFrom == "" {
					continue // projet créé par Symphony : déjà connu
				}
				if k.Name != found.Name {
					fail(repo.Path, fmt.Errorf("la fiche s'appelle maintenant %s, mais le projet %s existe pour ce repo : retirer %s de Symphony pour le reprendre sous son nouveau nom", found.Name, k.Name, k.Name))
					matched[k.Name] = true
					continue
				}
				checkTeam()
				if err := d.store.UpdateProject(found); err != nil {
					fail(repo.Path, err)
					failed = true
				}
				matched[k.Name] = true
				sr.Projects = append(sr.Projects, k.Name)
				continue
			}
			if k, ok := byName[found.Name]; ok {
				fail(repo.Path, fmt.Errorf("le nom %s est déjà pris par le projet du repo %s", found.Name, k.RepoPath))
				continue
			}
			checkTeam()
			if err := d.store.AddProject(found); err != nil {
				fail(repo.Path, fmt.Errorf("enregistrement du projet %s : %w", found.Name, err))
				failed = true
				continue
			}
			slog.Info("découverte : projet ajouté", "project", found.Name, "repo", repo.Path, "team", found.Team)
			k := KnownProject{Name: found.Name, RepoPath: repo.Path, SCMProvider: src.SCM, DiscoveredFrom: src.Key()}
			byName[k.Name], byRepo[src.SCM+"/"+repo.Path] = k, k
			matched[k.Name] = true
			added = true
			sr.Projects = append(sr.Projects, k.Name)
			sr.Imported = append(sr.Imported, k.Name)
		}
		report.Sources = append(report.Sources, sr)
	}

	// Projets découverts que leur source ne donne plus ; une source illisible
	// à ce scan ne fait pas d'orphelins
	sources := map[string]bool{}
	for _, src := range settings.Sources {
		sources[src.Key()] = true
	}
	for _, k := range known {
		if k.DiscoveredFrom == "" || matched[k.Name] {
			continue
		}
		if sources[k.DiscoveredFrom] && !scanned[k.DiscoveredFrom] {
			matched[k.Name] = true
			continue
		}
		report.Orphans = append(report.Orphans, DiscoveryOrphan{Project: k.Name, Source: k.DiscoveredFrom, Repo: k.RepoPath})
	}
	if report.Errors == nil {
		report.Errors = []DiscoveryError{}
	}
	if report.Orphans == nil {
		report.Orphans = []DiscoveryOrphan{}
	}

	d.mu.Lock()
	d.report = report
	d.matched = matched
	d.mu.Unlock()
	if failed {
		metrics.Reload("discovery", "error")
	} else {
		metrics.Reload("discovery", "ok")
	}
	if added && d.onChange != nil {
		d.onChange()
	}
}

// foundProject : le projet que décrit la fiche d'un repo
func foundProject(content string, src DiscoverySource, repo provider.Repo) (FoundProject, error) {
	f, err := ParseFiche(content, "")
	if err != nil {
		return FoundProject{}, err
	}
	if !ProjectNameRe.MatchString(f.Name) {
		return FoundProject{}, fmt.Errorf("metadata.name %q : minuscules, chiffres et tirets, 63 caractères max.", f.Name)
	}
	m := f.Service.Metadata
	port := 8080
	if p := m.Annotations[AnnotationPort]; p != "" {
		port, _ = strconv.Atoi(p) // vérifié par ParseFiche
	}
	return FoundProject{
		Name:        f.Name,
		Description: m.Description,
		Language:    f.Service.Spec.Language,
		Type:        f.Service.Spec.Type,
		Port:        port,
		Team:        m.Owner,
		Source:      src,
		Repo:        repo,
	}, nil
}
