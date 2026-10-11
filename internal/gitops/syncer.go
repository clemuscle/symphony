package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/symphony/internal/catalogue"
	"github.com/yourorg/symphony/internal/provider"
	"gopkg.in/yaml.v3"
)

// Syncer lit le catalogue dans le repo de config via le provider SCM par
// défaut, résolu à chaque appel : il suit les rechargements de configuration
type Syncer struct {
	providers *provider.Registry
	store     *catalogue.Store
	interval  time.Duration

	syncMu sync.Mutex // une synchronisation à la fois, sinon une ancienne pourrait écraser une récente

	mu     sync.RWMutex
	source catalogueSource
}

type catalogueSource struct {
	configRepo  string
	branch      string
	servicesDir string
}

func NewSyncer(providers *provider.Registry, store *catalogue.Store, configRepo, branch, servicesDir string) *Syncer {
	return &Syncer{
		providers: providers,
		store:     store,
		source:    catalogueSource{configRepo, branch, servicesDir},
		interval:  15 * time.Second,
	}
}

// SetSource change le repo du catalogue (rechargement de configuration) ;
// la synchronisation suivante lit le nouveau repo
func (s *Syncer) SetSource(configRepo, branch, servicesDir string) {
	s.mu.Lock()
	s.source = catalogueSource{configRepo, branch, servicesDir}
	s.mu.Unlock()
}

// current renvoie le SCM par défaut et la source du catalogue du moment
func (s *Syncer) current() (provider.SCMProvider, catalogueSource, error) {
	s.mu.RLock()
	src := s.source
	s.mu.RUnlock()
	scm, err := s.providers.SCM("")
	return scm, src, err
}

func (s *Syncer) Start() {
	_, src, _ := s.current()
	slog.Info("gitops syncer démarré", "repo", src.configRepo, "interval", s.interval)
	s.sync()
	ticker := time.NewTicker(s.interval)
	for range ticker.C {
		s.sync()
	}
}

func (s *Syncer) ForceSync() {
	s.sync()
}

// Register écrit la fiche d'un service dans le repo de config (services/<nom>.yaml)
// puis recharge le catalogue. Remplace le job CI register-service, qui
// demandait un token d'écriture dans chaque pipeline.
func (s *Syncer) Register(svc catalogue.Service) error {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2) // même indentation que les fiches écrites à la main
	if err := enc.Encode(svc); err != nil {
		return fmt.Errorf("sérialisation service %s: %w", svc.Metadata.Name, err)
	}
	content := buf.Bytes()
	scm, src, err := s.current()
	if err != nil {
		return err
	}
	filePath := path.Join(src.servicesDir, svc.Metadata.Name+".yaml")
	if err := scm.PushFile(src.configRepo, src.branch, filePath, string(content),
		fmt.Sprintf("feat: register %s [symphony]", svc.Metadata.Name)); err != nil {
		return fmt.Errorf("push %s: %w", filePath, err)
	}
	s.ForceSync()
	return nil
}

// Unregister retire la fiche d'un service du repo de config puis recharge le
// catalogue. Une fiche déjà absente n'est pas une erreur.
func (s *Syncer) Unregister(name string) error {
	scm, src, err := s.current()
	if err != nil {
		return err
	}
	filePath := path.Join(src.servicesDir, name+".yaml")
	err = scm.DeleteFile(src.configRepo, src.branch, filePath,
		fmt.Sprintf("chore: unregister %s [symphony]", name))
	if err != nil && !errors.Is(err, provider.ErrNotFound) {
		return fmt.Errorf("suppression %s: %w", filePath, err)
	}
	s.ForceSync()
	return nil
}

func (s *Syncer) sync() {
	s.syncMu.Lock()
	defer s.syncMu.Unlock()

	scm, src, err := s.current()
	if err != nil {
		slog.Warn("gitops sync: provider SCM", "error", err)
		return
	}
	files, err := scm.ListFiles(src.configRepo, src.branch, src.servicesDir)
	if err != nil {
		slog.Warn("gitops sync: liste fichiers", "error", err)
		return
	}

	var services []catalogue.Service
	for _, filePath := range files {
		if !strings.HasSuffix(filePath, ".yaml") && !strings.HasSuffix(filePath, ".yml") {
			continue
		}
		if strings.HasSuffix(filePath, ".gitkeep") {
			continue
		}

		content, err := scm.GetFileContent(src.configRepo, src.branch, filePath)
		if err != nil {
			slog.Warn("gitops sync: lecture fichier", "path", filePath, "error", err)
			continue
		}

		var svc catalogue.Service
		if err := yaml.Unmarshal([]byte(content), &svc); err != nil {
			slog.Warn("gitops sync: parse YAML", "path", filePath, "error", err)
			continue
		}
		if svc.Kind == "Service" && svc.Metadata.Name != "" {
			services = append(services, svc)
		}
	}

	before := s.store.Count()
	s.store.Replace(services)
	after := s.store.Count()

	if before != after {
		slog.Info("catalogue mis à jour", "services", after)
	}
}
