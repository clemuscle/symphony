package gitops

import (
	"log/slog"
	"strings"
	"time"

	"github.com/yourorg/symphony/internal/catalogue"
	"github.com/yourorg/symphony/internal/provider"
	"gopkg.in/yaml.v3"
)

type Syncer struct {
	scm        provider.SCMProvider
	store      *catalogue.Store
	configRepo string
	branch     string
	servicesDir string
	interval   time.Duration
	lastCommit string
}

func NewSyncer(scm provider.SCMProvider, store *catalogue.Store, configRepo, branch, servicesDir string) *Syncer {
	return &Syncer{
		scm:         scm,
		store:       store,
		configRepo:  configRepo,
		branch:      branch,
		servicesDir: servicesDir,
		interval:    15 * time.Second,
	}
}

func (s *Syncer) Start() {
	slog.Info("gitops syncer démarré", "repo", s.configRepo, "interval", s.interval)
	s.sync()
	ticker := time.NewTicker(s.interval)
	for range ticker.C {
		s.sync()
	}
}

func (s *Syncer) ForceSync() {
	s.lastCommit = "" // force le rechargement
	s.sync()
}

func (s *Syncer) sync() {
	files, err := s.scm.ListFiles(s.configRepo, s.branch, s.servicesDir)
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

		content, err := s.scm.GetFileContent(s.configRepo, s.branch, filePath)
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
