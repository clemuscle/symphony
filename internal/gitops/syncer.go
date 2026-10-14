package gitops

import (
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
	"gopkg.in/yaml.v3"
)

// DefaultCatalogueFile : fiche d'un projet, à la racine de son repo
const DefaultCatalogueFile = "catalog-info.yaml"

// ProjectRef : un projet dont le repo porte une fiche catalogue
type ProjectRef struct {
	Name        string
	RepoPath    string
	Branch      string
	SCMProvider string // vide = SCM par défaut
}

// Syncer construit le catalogue à partir des fiches des projets : chaque
// projet créé par Symphony a la sienne (catalog-info.yaml) dans son repo, et
// son équipe la modifie comme le reste de son code. Le SCM de chaque projet
// est résolu à chaque appel : le Syncer suit les rechargements de configuration.
type Syncer struct {
	providers *provider.Registry
	store     *catalogue.Store
	projects  func() ([]ProjectRef, error)
	interval  time.Duration

	syncMu sync.Mutex // une synchronisation à la fois, sinon une ancienne pourrait écraser une récente

	mu   sync.RWMutex
	file string
}

func NewSyncer(providers *provider.Registry, store *catalogue.Store, projects func() ([]ProjectRef, error), file string) *Syncer {
	if file == "" {
		file = DefaultCatalogueFile
	}
	return &Syncer{
		providers: providers,
		store:     store,
		projects:  projects,
		file:      file,
		interval:  15 * time.Second,
	}
}

// SetFile change le nom de la fiche (rechargement de configuration)
func (s *Syncer) SetFile(file string) {
	if file == "" {
		file = DefaultCatalogueFile
	}
	s.mu.Lock()
	s.file = file
	s.mu.Unlock()
}

// File : chemin de la fiche dans le repo d'un projet
func (s *Syncer) File() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.file
}

func (s *Syncer) Start() {
	slog.Info("catalogue : lecture des fiches des projets", "fichier", s.File(), "interval", s.interval)
	s.sync()
	ticker := time.NewTicker(s.interval)
	for range ticker.C {
		s.sync()
	}
}

func (s *Syncer) ForceSync() {
	s.sync()
}

// Forget retire un projet du catalogue sans attendre la synchronisation
// suivante (projet supprimé)
func (s *Syncer) Forget(name string) {
	s.store.Remove(name)
}

func (s *Syncer) sync() {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	refs, err := s.projects()
	if err != nil {
		slog.Warn("catalogue : liste des projets", "error", err)
		metrics.Reload("catalogue", "error")
		return
	}
	file := s.File()

	failed := false
	services := make([]catalogue.Service, 0, len(refs))
	for _, ref := range refs {
		svc, err := s.read(ref, file)
		switch {
		case errors.Is(err, provider.ErrNotFound):
			// Pas de fiche : le projet reste visible, sans métadonnées
			continue
		case err != nil:
			// Erreur passagère (SCM injoignable…) : la fiche connue est gardée
			slog.Warn("catalogue : lecture de la fiche", "project", ref.Name, "error", err)
			failed = true
			if old, ok := s.store.Get(ref.Name); ok {
				services = append(services, old)
			}
			continue
		}
		services = append(services, svc)
	}

	before := s.store.Count()
	s.store.Replace(services)
	if failed {
		metrics.Reload("catalogue", "error")
	} else {
		metrics.Reload("catalogue", "ok")
	}
	metrics.CatalogueServices.Set(float64(len(services)))
	if after := s.store.Count(); before != after {
		slog.Info("catalogue mis à jour", "services", after)
	}
}

// read lit et valide la fiche d'un projet. Le nom vient du projet, pas de la
// fiche : c'est par lui que l'API retrouve le service et ses actions.
func (s *Syncer) read(ref ProjectRef, file string) (catalogue.Service, error) {
	scm, err := s.providers.SCM(ref.SCMProvider)
	if err != nil {
		return catalogue.Service{}, err
	}
	content, err := scm.GetFileContent(ref.RepoPath, ref.Branch, file)
	if err != nil {
		return catalogue.Service{}, err
	}
	return ParseService(ref.Name, content)
}

// ParseService lit une fiche catalogue (kind: Service) et lui donne le nom du
// projet. Une fiche invalide est une erreur, pas une fiche vide.
func ParseService(name, content string) (catalogue.Service, error) {
	var svc catalogue.Service
	if err := yaml.Unmarshal([]byte(content), &svc); err != nil {
		return svc, err
	}
	if svc.Kind != "" && svc.Kind != "Service" {
		return svc, errors.New("kind " + svc.Kind + " : Service attendu")
	}
	if svc.Metadata.Name != "" && svc.Metadata.Name != name {
		slog.Debug("catalogue : metadata.name ignoré, nom du projet utilisé",
			"fiche", svc.Metadata.Name, "project", name)
	}
	if svc.APIVersion == "" {
		svc.APIVersion = "symphony/v1"
	}
	svc.Kind = "Service"
	svc.Metadata.Name = name
	svc.Metadata.Tags = cleanTags(svc.Metadata.Tags)
	return svc, nil
}

// cleanTags retire les étiquettes vides
func cleanTags(tags []string) []string {
	out := tags[:0]
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}
