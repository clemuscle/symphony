package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/gitops"
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
			"catalogue:\n  repo: root/symphony-config\n" +
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
	os.WriteFile(f, []byte("catalogue:\n  repo: root/c\ntemplates:\n  repo: root/local\n"), 0o600)
	cfg, err := loadConfig(f)
	if err != nil || cfg.Templates.Repo != "root/local" {
		t.Errorf("config = %+v, err = %v", cfg, err)
	}
}

func TestConfigWatcher(t *testing.T) {
	const v1 = `providers:
  scm:
    - id: gitlab-a
      type: gitlab
  ci:
    - id: ci-a
      type: gitlab_ci
catalogue:
  repo: root/symphony-config
`
	content, readErr := []byte(v1), error(nil)
	source := &configSource{read: func() ([]byte, error) { return content, readErr }}

	cfg, raw, err := source.load()
	if err != nil {
		t.Fatal(err)
	}
	providers, err := buildProviders(cfg)
	if err != nil {
		t.Fatal(err)
	}
	syncer := gitops.NewSyncer(providers, catalogue.NewStore(), "root/symphony-config", "main", "services")
	w := &configWatcher{
		source:    source,
		providers: providers,
		syncer:    syncer,
		templates: gitops.NewTemplateLoader(providers, "root/symphony-templates", "main", "templates"),
		last:      raw,
	}
	assertSCM := func(want string) {
		t.Helper()
		if got := providers.ListSCM(); len(got) != 1 || got[0] != want {
			t.Errorf("scm = %v, attendu [%s]", got, want)
		}
	}

	// Nouvelle configuration valide : appliquée à chaud
	content = []byte(strings.ReplaceAll(v1, "-a", "-b"))
	w.check()
	assertSCM("gitlab-b")
	if _, err := providers.CI("ci-a"); err == nil {
		t.Error("ci-a devrait avoir disparu")
	}

	// Configuration invalide : refusée, la précédente reste en place
	for _, bad := range []string{
		"providers: [pas une map",                                          // YAML
		"catalogue:\n  repo: root/symphony-config\n",                       // pas de SCM par défaut
		strings.ReplaceAll(v1, "repo: root/symphony-config", "repo: \"\""), // pas de catalogue
	} {
		content = []byte(bad)
		w.check()
		assertSCM("gitlab-b")
	}

	// Repo illisible : on garde la configuration en place
	readErr = errors.New("gitlab injoignable")
	w.check()
	assertSCM("gitlab-b")

	// De retour : la configuration (valide) est appliquée
	readErr = nil
	content = []byte(v1)
	w.check()
	assertSCM("gitlab-a")
}
