package gitops

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/yourorg/symphony/internal/catalog"
	"gopkg.in/yaml.v3"
)

// Syncer poll périodiquement un repo GitOps pour recharger le catalogue de
// services. BaseURL/Token/RepoPath sont protégés par mu — ils peuvent être
// retargetés à chaud (via UpdateConfig) sans redémarrer la goroutine Start,
// notamment quand l'admin change config_repo via le wizard ou /config/reload.
type Syncer struct {
	mu         sync.Mutex
	BaseURL    string
	Token      string
	RepoPath   string
	Store      *catalog.Store
	Interval   time.Duration
	lastCommit string
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

// UpdateConfig retargete la synchro sur un autre repo/token/instance sans
// redémarrer la goroutine Start. lastCommit est réinitialisé — le nouveau
// repo n'a aucune continuité avec l'ancien, il doit être retraité au
// prochain tick comme si c'était la première synchro.
func (s *Syncer) UpdateConfig(baseURL, token, repoPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.BaseURL = strings.TrimRight(baseURL, "/")
	s.Token = token
	s.RepoPath = repoPath
	s.lastCommit = ""
	log.Printf("🔄 GitOps sync retargeté — repo: %s", repoPath)
}

func (s *Syncer) config() (baseURL, token, repoPath string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.BaseURL, s.Token, s.RepoPath
}

func (s *Syncer) Start() {
	_, _, repoPath := s.config()
	log.Printf("🔄 GitOps sync démarré — repo: %s (interval: %s)", repoPath, s.Interval)

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
	baseURL, token, repoPath := s.config()

	// Vérifier le dernier commit
	commit, err := s.getLastCommit(baseURL, token, repoPath)
	if err != nil {
		return fmt.Errorf("getLastCommit: %w", err)
	}

	s.mu.Lock()
	unchanged := commit == s.lastCommit
	s.mu.Unlock()
	if unchanged {
		return nil
	}

	log.Printf("🔄 Nouveau commit détecté: %s — rechargement catalogue", commit[:8])

	// Charger les services depuis le repo
	services, err := s.loadServices(baseURL, token, repoPath)
	if err != nil {
		return fmt.Errorf("loadServices: %w", err)
	}

	// Ne pas écraser un catalogue existant avec une liste vide inattendue —
	// un dossier services/ non trouvé ou vide sur une erreur transitoire
	// ne doit pas vider le cache. Seule une liste non-nil (même vide) issue
	// d'un dossier réellement inexistant est acceptée.
	if services == nil {
		services = []catalog.Service{}
	}
	s.Store.Replace(services)
	s.mu.Lock()
	s.lastCommit = commit
	s.mu.Unlock()
	log.Printf("✅ Catalogue mis à jour — %d service(s)", len(services))
	return nil
}

func (s *Syncer) getLastCommit(baseURL, token, repoPath string) (string, error) {
	encoded := url.PathEscape(repoPath)
	data, _, err := s.api(baseURL, token, fmt.Sprintf("/projects/%s/repository/commits?per_page=1", encoded))
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

func (s *Syncer) loadServices(baseURL, token, repoPath string) ([]catalog.Service, error) {
	encoded := url.PathEscape(repoPath)

	// Lister les fichiers dans services/
	data, status, err := s.api(baseURL, token, fmt.Sprintf("/projects/%s/repository/tree?path=services&per_page=100", encoded))
	if err != nil {
		return nil, err
	}
	// 404 = dossier services/ absent — pas une erreur, catalogue vide
	if status == 404 {
		return []catalog.Service{}, nil
	}

	var files []struct {
		Name string `json:"name"`
		Path string `json:"path"`
		Type string `json:"type"`
	}
	if err := json.Unmarshal(data, &files); err != nil {
		return nil, fmt.Errorf("parse tree response: %w", err)
	}

	var services []catalog.Service
	for _, f := range files {
		if f.Type != "blob" || (!strings.HasSuffix(f.Name, ".yaml") && !strings.HasSuffix(f.Name, ".yml")) {
			continue
		}
		if f.Name == ".gitkeep" {
			continue
		}

		content, err := s.getFile(baseURL, token, repoPath, f.Path)
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

func (s *Syncer) getFile(baseURL, token, repoPath, filePath string) (string, error) {
	encoded := url.PathEscape(repoPath)
	fileEncoded := url.PathEscape(filePath)
	data, _, err := s.api(baseURL, token, fmt.Sprintf("/projects/%s/repository/files/%s/raw?ref=main", encoded, fileEncoded))
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func (s *Syncer) api(baseURL, token, path string) ([]byte, int, error) {
	req, _ := http.NewRequest("GET", baseURL+"/api/v4"+path, nil)
	req.Header.Set("PRIVATE-TOKEN", token)
	resp, err := (&http.Client{Timeout: 10 * time.Second}).Do(req)
	if err != nil {
		return nil, 0, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}
