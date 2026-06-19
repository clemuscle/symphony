package gitops

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
	"github.com/yourorg/symphony/internal/catalog"
)

type Syncer struct {
	BaseURL     string
	Token       string
	RepoPath    string
	Store       *catalog.Store
	Interval    time.Duration
	lastCommit  string
}

func NewSyncer(baseURL, token, repoPath string, store *catalog.Store) *Syncer {
	return &Syncer{
		BaseURL:  strings.TrimRight(baseURL, "/"),
		Token:    token,
		RepoPath: repoPath,
		Store:    store,
		Interval: 15 * time.Second,
	}
}

func (s *Syncer) Start() {
	log.Printf("🔄 GitOps sync démarré — repo: %s (interval: %s)", s.RepoPath, s.Interval)

	// Chargement initial
	if err := s.sync(); err != nil {
		log.Printf("⚠️  sync initial: %v", err)
	}

	ticker := time.NewTicker(s.Interval)
	for range ticker.C {
		if err := s.sync(); err != nil {
			log.Printf("⚠️  sync: %v", err)
		}
	}
}

func (s *Syncer) sync() error {
	// Vérifier le dernier commit
	commit, err := s.getLastCommit()
	if err != nil {
		return fmt.Errorf("getLastCommit: %w", err)
	}

	// Rien de nouveau
	if commit == s.lastCommit {
		return nil
	}

	log.Printf("🔄 Nouveau commit détecté: %s — rechargement catalogue", commit[:8])

	// Charger les services depuis le repo
	services, err := s.loadServices()
	if err != nil {
		return fmt.Errorf("loadServices: %w", err)
	}

	s.Store.Replace(services)
	s.lastCommit = commit
	log.Printf("✅ Catalogue mis à jour — %d service(s)", len(services))
	return nil
}

func (s *Syncer) getLastCommit() (string, error) {
	encoded := url.PathEscape(s.RepoPath)
	data, err := s.api(fmt.Sprintf("/projects/%s/repository/commits?per_page=1", encoded))
	if err != nil {
		return "", err
	}

	var commits []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(data, &commits); err != nil || len(commits) == 0 {
		return "", fmt.Errorf("no commits found")
	}
	return commits[0].ID, nil
}

func (s *Syncer) loadServices() ([]catalog.Service, error) {
	encoded := url.PathEscape(s.RepoPath)

	// Lister les fichiers dans services/
	data, err := s.api(fmt.Sprintf("/projects/%s/repository/tree?path=services&per_page=100", encoded))
	if err != nil {
		return nil, err
	}

	var files []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	json.Unmarshal(data, &files)

	var services []catalog.Service
	for _, f := range files {
		if f.Type != "blob" || (!strings.HasSuffix(f.Name, ".yaml") && !strings.HasSuffix(f.Name, ".yml")) {
			continue
		}
		if f.Name == ".gitkeep" {
			continue
		}

		content, err := s.getFile(f.Path)
		if err != nil {
			log.Printf("⚠️  lecture %s: %v", f.Path, err)
			continue
		}

		var svc catalog.Service
		if err := yaml.Unmarshal([]byte(content), &svc); err != nil {
			log.Printf("⚠️  parse %s: %v", f.Path, err)
			continue
		}
		if svc.Kind == "Service" {
			services = append(services, svc)
		}
	}
	return services, nil
}

func (s *Syncer) getFile(filePath string) (string, error) {
	encoded := url.PathEscape(s.RepoPath)
	fileEncoded := url.PathEscape(filePath)
	data, err := s.api(fmt.Sprintf("/projects/%s/repository/files/%s/raw?ref=main", encoded, fileEncoded))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Syncer) api(path string) ([]byte, error) {
	req, _ := http.NewRequest("GET", s.BaseURL+"/api/v4"+path, nil)
	req.Header.Set("PRIVATE-TOKEN", s.Token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	return io.ReadAll(resp.Body)
}
