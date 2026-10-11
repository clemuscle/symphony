package main

import (
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// fakeGitLab sert les fichiers bruts d'un repo (chemin → contenu), comme
// GET /api/v4/projects/<repo>/repository/files/<fichier>/raw
func fakeGitLab(t *testing.T, repo string, files map[string]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		for name, content := range files {
			want := "/api/v4/projects/" + strings.ReplaceAll(repo, "/", "%2F") +
				"/repository/files/" + strings.ReplaceAll(name, "/", "%2F") + "/raw"
			if r.URL.EscapedPath() == want && r.URL.Query().Get("ref") == "main" {
				w.Write([]byte(content))
				return
			}
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func writeBootstrap(t *testing.T, gitlabURL, path string) string {
	f := filepath.Join(t.TempDir(), "symphony.yaml")
	content := "config_source:\n" +
		"  scm:\n    type: gitlab\n    config:\n      url: " + gitlabURL + "\n" +
		"  repo: root/symphony-config\n" +
		"  path: " + path + "\n"
	if err := os.WriteFile(f, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return f
}

func TestLoadConfigFromGit(t *testing.T) {
	srv := fakeGitLab(t, "root/symphony-config", map[string]string{
		"symphony.yaml": "providers:\n  scm:\n    - id: gitlab-local\n      type: gitlab\n" +
			"templates:\n  repo: root/symphony-templates\n",
		"boucle.yaml": "config_source:\n  repo: root/autre\n",
	})

	cfg, err := loadConfig(writeBootstrap(t, srv.URL, "symphony.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.Providers.SCM) != 1 || cfg.Providers.SCM[0].ID != "gitlab-local" ||
		cfg.Templates.Repo != "root/symphony-templates" {
		t.Errorf("config = %+v", cfg)
	}

	// La configuration distante ne peut pas renvoyer vers une autre source
	if _, err := loadConfig(writeBootstrap(t, srv.URL, "boucle.yaml")); err == nil ||
		!strings.Contains(err.Error(), "config_source") {
		t.Errorf("erreur config_source attendue, obtenu %v", err)
	}

	if _, err := loadConfig(writeBootstrap(t, srv.URL, "absent.yaml")); err == nil {
		t.Error("erreur attendue pour un fichier absent")
	}
}

func TestLoadConfigLocal(t *testing.T) {
	f := filepath.Join(t.TempDir(), "symphony.yaml")
	os.WriteFile(f, []byte("templates:\n  repo: root/local\n"), 0o600)
	cfg, err := loadConfig(f)
	if err != nil || cfg.Templates.Repo != "root/local" {
		t.Errorf("config = %+v, err = %v", cfg, err)
	}
}
