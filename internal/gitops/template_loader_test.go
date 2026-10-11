package gitops

import (
	"errors"
	"sort"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
	githubci "github.com/clemuscle/symphony/internal/provider/ci/githubactions"
	gitlabci "github.com/clemuscle/symphony/internal/provider/ci/gitlabci"
	k8srt "github.com/clemuscle/symphony/internal/provider/runtime/kubernetes"
	gitlabscm "github.com/clemuscle/symphony/internal/provider/scm/gitlab"
)

// fakeSCM sert un repo en mémoire (chemin → contenu) ; err simule un repo illisible
type fakeSCM struct {
	provider.SCMProvider
	files map[string]string
	err   error
}

func (f *fakeSCM) ID() string       { return "fake" }
func (f *fakeSCM) Platform() string { return "gitlab" }

func (f *fakeSCM) ListFiles(repo, branch, dir string) ([]string, error) {
	if f.err != nil {
		return nil, f.err
	}
	var paths []string
	for p := range f.files {
		if strings.HasPrefix(p, dir+"/") {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)
	return paths, nil
}

func (f *fakeSCM) GetFileContent(repo, branch, filePath string) (string, error) {
	content, ok := f.files[filePath]
	if !ok {
		return "", provider.ErrNotFound
	}
	return content, nil
}

// testProviders : scm est le SCM par défaut (celui qui lit les templates),
// gitlab-local celui que les templates désignent
func testProviders(scm provider.SCMProvider) *provider.Registry {
	r := provider.NewRegistry()
	r.RegisterSCM(scm, true)
	r.RegisterSCM(gitlabscm.New("gitlab-local", "http://gitlab.local", ""), false)
	r.RegisterCI(gitlabci.New("gitlab-ci", "http://gitlab.local", ""), true)
	r.RegisterCI(githubci.New("github-actions", ""), false)
	r.RegisterRuntime(k8srt.New("k8s"), true)
	return r
}

const goTemplate = `apiVersion: symphony/v1
kind: GoldenPath
metadata:
  name: api-rest-go
  title: "API REST Go"
  description: "API Go"
  icon: go
spec:
  language: go
  type: api
  providers:
    scm: gitlab-local
    ci: gitlab-ci
  ci_template: ci.gitlab-ci.yml
`

func TestParseTemplate(t *testing.T) {
	gp, err := parseTemplate("templates/api-rest-go", goTemplate)
	if err != nil {
		t.Fatal(err)
	}
	want := GoldenPath{
		ID: "api-rest-go", Name: "API REST Go", Description: "API Go", Icon: "go",
		Language: "go", Type: "api", SCMProvider: "gitlab-local", CIProvider: "gitlab-ci",
		Skeleton:   "templates/api-rest-go/skeleton",
		CITemplate: "templates/api-rest-go/ci.gitlab-ci.yml",
	}
	if gp != want {
		t.Errorf("parseTemplate = %+v\nattendu %+v", gp, want)
	}
}

func TestParseTemplateInvalid(t *testing.T) {
	cases := map[string]string{
		"kind":          strings.Replace(goTemplate, "kind: GoldenPath", "kind: Service", 1),
		"nom/dossier":   strings.Replace(goTemplate, "name: api-rest-go", "name: autre", 1),
		"nom invalide":  strings.Replace(goTemplate, "name: api-rest-go", "name: Api_Go", 1),
		"ci manquant":   strings.Replace(goTemplate, "  ci_template: ci.gitlab-ci.yml\n", "", 1),
		"clé inconnue":  strings.Replace(goTemplate, "  language: go", "  langage: go", 1),
		"sortie ..":     strings.Replace(goTemplate, "ci.gitlab-ci.yml", "../api-rest-node/ci.gitlab-ci.yml", 1),
		"chemin absolu": strings.Replace(goTemplate, "ci.gitlab-ci.yml", "/ci.gitlab-ci.yml", 1),
		"skeleton = .":  goTemplate + "  skeleton: .\n",
	}
	for name, content := range cases {
		if _, err := parseTemplate("templates/api-rest-go", content); err == nil {
			t.Errorf("%s : erreur attendue", name)
		}
	}
}

func TestReload(t *testing.T) {
	scm := &fakeSCM{files: map[string]string{
		"templates/api-rest-go/template.yaml":        goTemplate,
		"templates/api-rest-go/ci.gitlab-ci.yml":     "stages: [test]",
		"templates/api-rest-go/skeleton/go.mod":      "module {{SERVICE_NAME}}",
		"templates/api-rest-go/skeleton/cmd/main.go": "package main",
		// template.yaml dans un skeleton : ce n'est pas un golden path
		"templates/api-rest-go/skeleton/template.yaml": "kind: Autre",
		// CI GitHub sur un SCM GitLab : ignoré
		"templates/api-github/template.yaml": strings.NewReplacer(
			"api-rest-go", "api-github", "ci: gitlab-ci", "ci: github-actions").Replace(goTemplate),
		"templates/api-github/ci.gitlab-ci.yml": "x",
		"templates/api-github/skeleton/a":       "x",
		// skeleton absent : ignoré
		"templates/sans-skeleton/template.yaml":    strings.Replace(goTemplate, "api-rest-go", "sans-skeleton", 1),
		"templates/sans-skeleton/ci.gitlab-ci.yml": "x",
	}}
	l := NewTemplateLoader(testProviders(scm), "root/symphony-templates", "main", "templates")

	l.Reload()
	gps := l.GetGoldenPaths()
	if len(gps) != 1 || gps[0].ID != "api-rest-go" {
		t.Fatalf("golden paths = %+v, attendu [api-rest-go]", gps)
	}

	files, err := l.GetScaffoldFiles(gps[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(files) != 3 || files["go.mod"] != "module {{SERVICE_NAME}}" || files["cmd/main.go"] != "package main" {
		t.Errorf("scaffold = %v", files)
	}
	if ci, err := l.GetCITemplate(gps[0]); err != nil || ci != "stages: [test]" {
		t.Errorf("ci template = %q, %v", ci, err)
	}

	// Repo illisible : la liste précédente est conservée
	scm.err = errors.New("gitlab injoignable")
	l.Reload()
	if _, err := l.GetGoldenPath("api-rest-go"); err != nil {
		t.Errorf("liste précédente perdue : %v", err)
	}

	// Template supprimé du repo : il disparaît au rechargement suivant
	scm.err = nil
	delete(scm.files, "templates/api-rest-go/template.yaml")
	l.Reload()
	if gps := l.GetGoldenPaths(); len(gps) != 0 {
		t.Errorf("golden paths = %+v, attendu aucun", gps)
	}
}

// Changement de repo de templates (rechargement de configuration) : les
// golden paths déjà chargés restent lus dans leur repo d'origine
func TestSetSource(t *testing.T) {
	scm := &fakeSCM{files: map[string]string{
		"templates/api-rest-go/template.yaml":    goTemplate,
		"templates/api-rest-go/ci.gitlab-ci.yml": "ancien",
		"templates/api-rest-go/skeleton/go.mod":  "x",
		"autre/api-rest-go/template.yaml":        goTemplate,
		"autre/api-rest-go/ci.gitlab-ci.yml":     "nouveau",
		"autre/api-rest-go/skeleton/go.mod":      "x",
	}}
	l := NewTemplateLoader(testProviders(scm), "root/symphony-templates", "main", "templates")
	l.Reload()
	old, err := l.GetGoldenPath("api-rest-go")
	if err != nil {
		t.Fatal(err)
	}

	l.SetSource("root/symphony-templates", "main", "autre")
	l.Reload()
	gp, _ := l.GetGoldenPath("api-rest-go")
	if ci, _ := l.GetCITemplate(*gp); ci != "nouveau" {
		t.Errorf("après SetSource, ci = %q, attendu nouveau", ci)
	}
	if ci, _ := l.GetCITemplate(*old); ci != "ancien" {
		t.Errorf("golden path chargé avant SetSource, ci = %q, attendu ancien", ci)
	}
}
