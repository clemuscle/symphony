package gitops

import (
	"errors"
	"os"
	"reflect"
	"sort"
	"strings"
	"testing"
	"testing/fstest"

	"github.com/clemuscle/symphony/internal/provider"
	githubci "github.com/clemuscle/symphony/internal/provider/ci/githubactions"
	gitlabci "github.com/clemuscle/symphony/internal/provider/ci/gitlabci"
	dockerrt "github.com/clemuscle/symphony/internal/provider/runtime/docker"
	gitlabscm "github.com/clemuscle/symphony/internal/provider/scm/gitlab"
	officialstacks "github.com/clemuscle/symphony/stacks"
	"gopkg.in/yaml.v3"
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
		if dir == "" || strings.HasPrefix(p, dir+"/") {
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

// testProviders : scm est le SCM par défaut (celui qui lit les stacks),
// gitlab-local celui que les stacks désignent
func testProviders(scm provider.SCMProvider) *provider.Registry {
	r := provider.NewRegistry()
	r.RegisterSCM(scm, true)
	r.RegisterSCM(gitlabscm.New("gitlab-local", "http://gitlab.local", ""), false)
	r.RegisterCI(gitlabci.New("gitlab-ci", "http://gitlab.local", ""), true)
	r.RegisterCI(githubci.New("github-actions", ""), false)
	r.RegisterRuntime(dockerrt.New("docker-local", "/var/run/docker.sock"), true)
	return r
}

const goStack = `apiVersion: symphony/v1
kind: Stack
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

// legacyTemplate : l'ancien format (template.yaml, kind GoldenPath)
var legacyTemplate = strings.Replace(goStack, "kind: Stack", "kind: GoldenPath", 1)

func TestParseStack(t *testing.T) {
	s, err := parseStack("stacks/api-rest-go", StackFile, goStack)
	if err != nil {
		t.Fatal(err)
	}
	if s.ID != "api-rest-go" || s.Name != "API REST Go" || s.SCMProvider != "gitlab-local" ||
		s.Skeleton != "stacks/api-rest-go/skeleton" || s.ciTemplates[""] != "stacks/api-rest-go/ci.gitlab-ci.yml" ||
		s.Legacy || len(s.Parameters) != 0 || s.Parameters == nil {
		t.Errorf("parseStack = %+v", s)
	}
	// L'ancien format est lu, et signalé
	old, err := parseStack("templates/api-rest-go", LegacyStackFile, legacyTemplate)
	if err != nil || !old.Legacy {
		t.Errorf("template.yaml : %+v, %v", old, err)
	}
}

func TestParseStackInvalid(t *testing.T) {
	cases := map[string]struct{ file, content string }{
		"kind":                {StackFile, strings.Replace(goStack, "kind: Stack", "kind: Service", 1)},
		"GoldenPath en stack": {StackFile, legacyTemplate},
		"Stack en template":   {LegacyStackFile, goStack},
		"nom/dossier":         {StackFile, strings.Replace(goStack, "name: api-rest-go", "name: autre", 1)},
		"nom invalide":        {StackFile, strings.Replace(goStack, "name: api-rest-go", "name: Api_Go", 1)},
		"ci manquant":         {StackFile, strings.Replace(goStack, "  ci_template: ci.gitlab-ci.yml\n", "", 1)},
		"ci en double":        {StackFile, goStack + "  ci_templates: {gitlab: ci.yml}\n"},
		"plateforme inconnue": {StackFile, strings.Replace(goStack, "  ci_template: ci.gitlab-ci.yml\n", "  ci_templates: {bitbucket: ci.yml}\n", 1)},
		"clé inconnue":        {StackFile, strings.Replace(goStack, "  language: go", "  langage: go", 1)},
		"sortie ..":           {StackFile, strings.Replace(goStack, "ci.gitlab-ci.yml", "../api-rest-node/ci.gitlab-ci.yml", 1)},
		"chemin absolu":       {StackFile, strings.Replace(goStack, "ci.gitlab-ci.yml", "/ci.gitlab-ci.yml", 1)},
		"skeleton = .":        {StackFile, goStack + "  skeleton: .\n"},
		"paramètre":           {StackFile, goStack + "  parameters:\n    - {name: x, type: date}\n"},
		"files sans when":     {StackFile, goStack + "  files:\n    - {path: a}\n"},
		"files when inconnu":  {StackFile, goStack + "  files:\n    - {path: a, when: {x: true}}\n"},
		"files hors repo":     {StackFile, goStack + "  parameters:\n    - {name: x, type: boolean}\n  files:\n    - {path: ../a, when: {x: true}}\n"},
		"helm et kustomize":   {StackFile, goStack + "  kubernetes: {helm: a, kustomize: b}\n"},
		"kubernetes vide":     {StackFile, goStack + "  kubernetes: {}\n"},
		"kubernetes absolu":   {StackFile, goStack + "  kubernetes: {helm: /chart}\n"},
		"infra sans dossier":  {StackFile, goStack + "  infra: {env: {A: {secret: db, key: uri}}}\n"},
		"infra variable":      {StackFile, goStack + "  infra: {kubernetes: i, env: {A-B: {secret: db, key: uri}}}\n"},
		"infra secret":        {StackFile, goStack + "  infra: {kubernetes: i, env: {A: {secret: Db_App, key: uri}}}\n"},
		"infra sans clé":      {StackFile, goStack + "  infra: {kubernetes: i, env: {A: {secret: db}}}\n"},
		"infra et helm":       {StackFile, goStack + "  kubernetes: {helm: c}\n  infra: {kubernetes: i}\n"},
	}
	for name, c := range cases {
		if _, err := parseStack("stacks/api-rest-go", c.file, c.content); err == nil {
			t.Errorf("%s : erreur attendue", name)
		}
	}
}

func TestCheckFiles(t *testing.T) {
	stack := goStack + "  kubernetes: {helm: deploy/chart}\n"
	exists := map[string]bool{
		"stacks/api-rest-go/ci.gitlab-ci.yml":                  true,
		"stacks/api-rest-go/skeleton/go.mod.tmpl":              true,
		"stacks/api-rest-go/skeleton/deploy/chart/Chart.yaml":  true,
		"stacks/api-rest-go/skeleton/deploy/chart/values.yaml": true,
	}
	if _, err := CheckStack("stacks/api-rest-go", StackFile, stack, exists, nil); err != nil {
		t.Errorf("stack valide refusée : %v", err)
	}
	infra := goStack + "  infra: {kubernetes: deploy/infra}\n"
	if _, err := CheckStack("stacks/api-rest-go", StackFile, infra, exists, nil); err == nil || !strings.Contains(err.Error(), "deploy/infra") {
		t.Errorf("dossier de l'infra absent : %v", err)
	}
	exists["stacks/api-rest-go/skeleton/deploy/infra/cluster.yaml.tmpl"] = true
	if _, err := CheckStack("stacks/api-rest-go", StackFile, infra, exists, nil); err != nil {
		t.Errorf("infra présente refusée : %v", err)
	}
	delete(exists, "stacks/api-rest-go/skeleton/deploy/chart/Chart.yaml")
	if _, err := CheckStack("stacks/api-rest-go", StackFile, stack, exists, nil); err == nil || !strings.Contains(err.Error(), "Chart.yaml") {
		t.Errorf("chart absent : %v", err)
	}
}

// Une stack sans providers prend ceux par défaut, et la CI de leur plateforme
func TestResolveProviders(t *testing.T) {
	stack := strings.Replace(goStack, "  providers:\n    scm: gitlab-local\n    ci: gitlab-ci\n  ci_template: ci.gitlab-ci.yml\n",
		"  ci_templates:\n    gitlab: ci.gitlab-ci.yml\n    github: ci.github.yml\n", 1)
	s, err := parseStack("stacks/api-rest-go", StackFile, stack)
	if err != nil {
		t.Fatal(err)
	}
	r, err := resolveProviders(s, testProviders(&fakeSCM{}))
	if err != nil {
		t.Fatal(err)
	}
	if r.SCMProvider != "fake" || r.CIProvider != "gitlab-ci" || r.RTProvider != "docker-local" || r.RegProvider != "" ||
		r.CITemplate != "stacks/api-rest-go/ci.gitlab-ci.yml" {
		t.Errorf("providers résolus : %+v", r)
	}
	// Pas de CI pour la plateforme du SCM : refusée
	delete(s.ciTemplates, "gitlab")
	if _, err := resolveProviders(s, testProviders(&fakeSCM{})); err == nil || !strings.Contains(err.Error(), "gitlab") {
		t.Errorf("CI absente : %v", err)
	}
}

func TestReload(t *testing.T) {
	scm := &fakeSCM{files: map[string]string{
		"stacks/api-rest-go/stack.yaml":                    goStack,
		"stacks/api-rest-go/ci.gitlab-ci.yml":              "stages: [test]",
		"stacks/api-rest-go/skeleton/go.mod.tmpl":          "module {{SERVICE_NAME}}",
		"stacks/api-rest-go/skeleton/cmd/main.go":          "package main",
		"stacks/api-rest-go/skeleton/stack.yaml":           "kind: Autre", // dans un squelette : pas une stack
		"stacks/ancien/template.yaml":                      strings.Replace(legacyTemplate, "api-rest-go", "ancien", 1),
		"stacks/ancien/ci.gitlab-ci.yml":                   "x",
		"stacks/ancien/skeleton/a":                         "x",
		"stacks/api-github/stack.yaml":                     strings.NewReplacer("api-rest-go", "api-github", "ci: gitlab-ci", "ci: github-actions").Replace(goStack),
		"stacks/api-github/ci.gitlab-ci.yml":               "x",
		"stacks/api-github/skeleton/a":                     "x",
		"stacks/sans-skeleton/stack.yaml":                  strings.Replace(goStack, "api-rest-go", "sans-skeleton", 1),
		"stacks/sans-skeleton/ci.gitlab-ci.yml":            "x",
		"stacks/api-rest-go/skeleton/deploy/template.yaml": "kind: Autre",
	}}
	l := NewStackLoader(testProviders(scm), nil, StackSource{Repo: "root/stacks", Branch: "main", Path: "stacks"})

	l.Reload()
	ids := stackIDs(l.GetStacks())
	if !reflect.DeepEqual(ids, []string{"ancien", "api-rest-go"}) {
		t.Fatalf("stacks = %v", ids)
	}
	// Les refusées sont rapportées avec leur raison, l'ancien format signalé
	report := l.Report()
	if report.Error != "" || report.LoadedAt.IsZero() || len(report.Refused) != 2 ||
		report.Refused[0].File != "stacks/api-github/stack.yaml" ||
		!strings.Contains(report.Refused[0].Error, "incompatible") ||
		!strings.Contains(report.Refused[1].Error, "skeleton") ||
		len(report.Warnings) != 1 || !strings.Contains(report.Warnings[0], "stacks/ancien/template.yaml") {
		t.Errorf("rapport = %+v", report)
	}

	s, _ := l.GetStack("api-rest-go")
	files, err := l.GetScaffoldFiles(*s)
	if err != nil {
		t.Fatal(err)
	}
	// .tmpl retiré du chemin dans le repo créé
	if len(files) != 4 || files["go.mod"] != "module {{SERVICE_NAME}}" || files["cmd/main.go"] != "package main" {
		t.Errorf("scaffold = %v", files)
	}
	if ci, err := l.GetCITemplate(*s); err != nil || ci != "stages: [test]" {
		t.Errorf("ci template = %q, %v", ci, err)
	}

	// Repo illisible : la liste précédente est conservée
	scm.err = errors.New("gitlab injoignable")
	l.Reload()
	if _, err := l.GetStack("api-rest-go"); err != nil {
		t.Errorf("liste précédente perdue : %v", err)
	}
	if r := l.Report(); !strings.Contains(r.Error, "gitlab injoignable") || len(r.Refused) != 2 {
		t.Errorf("échec non rapporté : %+v", r)
	}

	// Stack supprimée du repo : elle disparaît au rechargement suivant
	scm.err = nil
	delete(scm.files, "stacks/api-rest-go/stack.yaml")
	l.Reload()
	if ids := stackIDs(l.GetStacks()); !reflect.DeepEqual(ids, []string{"ancien"}) {
		t.Errorf("stacks = %v", ids)
	}
	if r := l.Report(); r.Error != "" {
		t.Errorf("erreur non effacée après un rechargement réussi : %q", r.Error)
	}
}

func stackIDs(stacks []Stack) []string {
	ids := []string{}
	for _, s := range stacks {
		ids = append(ids, s.ID)
	}
	return ids
}

// officialFS : un catalogue officiel de test
func officialFS() fstest.MapFS {
	stack := strings.Replace(goStack, "  providers:\n    scm: gitlab-local\n    ci: gitlab-ci\n", "", 1)
	return fstest.MapFS{
		"api-rest-go/stack.yaml":       {Data: []byte(stack)},
		"api-rest-go/ci.gitlab-ci.yml": {Data: []byte("officielle")},
		"api-rest-go/skeleton/a":       {Data: []byte("a")},
		"worker/stack.yaml":            {Data: []byte(strings.Replace(stack, "api-rest-go", "worker", 1))},
		"worker/ci.gitlab-ci.yml":      {Data: []byte("x")},
		"worker/skeleton/b.tmpl":       {Data: []byte("b")},
		"README.md":                    {Data: []byte("x")},
	}
}

// Catalogue officiel : seules les stacks exposées sont proposées, et une
// stack du repo remplace l'officielle du même nom
func TestOfficialStacks(t *testing.T) {
	scm := &fakeSCM{files: map[string]string{}}
	l := NewStackLoader(testProviders(scm), officialFS(), StackSource{Official: []string{"worker", "inconnue"}})
	l.Reload()
	if ids := stackIDs(l.GetStacks()); !reflect.DeepEqual(ids, []string{"worker"}) {
		t.Fatalf("stacks = %v", ids)
	}
	if r := l.Report(); len(r.Refused) != 1 || r.Refused[0].File != "officiel:inconnue" {
		t.Errorf("rapport = %+v", r)
	}
	w, _ := l.GetStack("worker")
	if !w.Official || w.SCMProvider != "fake" || w.Repo != "" {
		t.Errorf("stack officielle = %+v", w)
	}
	if files, err := l.GetScaffoldFiles(*w); err != nil || files["b"] != "b" {
		t.Errorf("squelette officiel = %v, %v", files, err)
	}

	scm.files = map[string]string{
		"stacks/api-rest-go/stack.yaml":       goStack,
		"stacks/api-rest-go/ci.gitlab-ci.yml": "du repo",
		"stacks/api-rest-go/skeleton/a":       "x",
	}
	l.SetSource(StackSource{Repo: "root/stacks", Branch: "main", Path: "stacks", Official: []string{"*"}})
	l.Reload()
	if ids := stackIDs(l.GetStacks()); !reflect.DeepEqual(ids, []string{"api-rest-go", "worker"}) {
		t.Fatalf("stacks = %v", ids)
	}
	s, _ := l.GetStack("api-rest-go")
	if ci, _ := l.GetCITemplate(*s); s.Official || ci != "du repo" {
		t.Errorf("la stack du repo doit remplacer l'officielle : %+v", s)
	}
	if r := l.Report(); len(r.Warnings) != 1 || !strings.Contains(r.Warnings[0], "remplace") {
		t.Errorf("remplacement non signalé : %+v", r)
	}
	if names := OfficialNames(officialFS()); !reflect.DeepEqual(names, []string{"api-rest-go", "worker"}) {
		t.Errorf("OfficialNames = %v", names)
	}
}

// Changement de source (rechargement de configuration) : les stacks déjà
// chargées restent lues dans leur repo d'origine
func TestSetSource(t *testing.T) {
	scm := &fakeSCM{files: map[string]string{
		"stacks/api-rest-go/stack.yaml":       goStack,
		"stacks/api-rest-go/ci.gitlab-ci.yml": "ancien",
		"stacks/api-rest-go/skeleton/go.mod":  "x",
		"autre/api-rest-go/stack.yaml":        goStack,
		"autre/api-rest-go/ci.gitlab-ci.yml":  "nouveau",
		"autre/api-rest-go/skeleton/go.mod":   "x",
	}}
	l := NewStackLoader(testProviders(scm), nil, StackSource{Repo: "root/stacks", Branch: "main", Path: "stacks"})
	l.Reload()
	old, err := l.GetStack("api-rest-go")
	if err != nil {
		t.Fatal(err)
	}

	l.SetSource(StackSource{Repo: "root/stacks", Branch: "main", Path: "autre"})
	l.Reload()
	s, _ := l.GetStack("api-rest-go")
	if ci, _ := l.GetCITemplate(*s); ci != "nouveau" {
		t.Errorf("après SetSource, ci = %q, attendu nouveau", ci)
	}
	if ci, _ := l.GetCITemplate(*old); ci != "ancien" {
		t.Errorf("stack chargée avant SetSource, ci = %q, attendu ancien", ci)
	}
}

// Le catalogue officiel embarqué est valide, avec les providers par défaut
// d'une GitLab comme d'une GitHub
func TestOfficialCatalogue(t *testing.T) {
	names := OfficialNames(officialstacks.FS())
	if len(names) < 4 {
		t.Fatalf("catalogue officiel : %v", names)
	}
	for _, platform := range []string{"gitlab", "github"} {
		r := provider.NewRegistry()
		if platform == "gitlab" {
			r.RegisterSCM(&fakeSCM{}, true)
			r.RegisterCI(gitlabci.New("gitlab-ci", "http://gitlab.local", ""), true)
		} else {
			r.RegisterSCM(&githubSCM{}, true)
			r.RegisterCI(githubci.New("github-actions", ""), true)
		}
		r.RegisterRuntime(dockerrt.New("docker", "/var/run/docker.sock"), true)
		l := NewStackLoader(r, officialstacks.FS(), StackSource{Official: []string{"*"}})
		l.Reload()
		if rep := l.Report(); len(rep.Refused) > 0 {
			t.Errorf("%s : stacks officielles refusées : %+v", platform, rep.Refused)
		}
		for _, s := range l.GetStacks() {
			files, err := l.GetScaffoldFiles(s)
			if err != nil || len(files) == 0 {
				t.Errorf("%s : squelette de %s : %v", platform, s.ID, err)
			}
			// Les valeurs par défaut suffisent à créer un projet
			if _, err := s.Resolve(nil); err != nil {
				t.Errorf("%s : paramètres par défaut : %v", s.ID, err)
			}
		}
	}
}

type githubSCM struct{ fakeSCM }

func (*githubSCM) Platform() string { return "github" }

// TestReferenceStack : la stack de référence est valide et renseigne chaque
// champ du format ; un champ ajouté sans être documenté échoue ici
func TestReferenceStack(t *testing.T) {
	raw, err := os.ReadFile("../../documentation/reference/stack.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := parseStack("stacks/api-rest-go", StackFile, string(raw)); err != nil {
		t.Fatalf("référence invalide : %v", err)
	}
	var doc stackDoc
	yaml.Unmarshal(raw, &doc)
	// Clés exclusives d'une autre : documentées en commentaire
	exclusive := map[string]string{"spec.ci_template": "# ci_template:", "spec.kubernetes.helm": "# helm:"}
	missing := func(name string) {
		if c, ok := exclusive[name]; ok && strings.Contains(string(raw), c) {
			return
		}
		t.Errorf("%s absent de la référence", name)
	}
	var walk func(prefix string, v reflect.Value)
	walk = func(prefix string, v reflect.Value) {
		switch v.Kind() {
		case reflect.Pointer:
			if v.IsNil() {
				missing(strings.TrimSuffix(prefix, "."))
				return
			}
			walk(prefix, v.Elem())
		case reflect.Slice:
			if v.Len() == 0 {
				missing(strings.TrimSuffix(prefix, "."))
				return
			}
			// Chaque champ d'un élément est renseigné par au moins un élément
			elem := v.Type().Elem()
			if elem.Kind() != reflect.Struct {
				return
			}
			for i := 0; i < elem.NumField(); i++ {
				set := false
				for j := 0; j < v.Len(); j++ {
					set = set || !v.Index(j).Field(i).IsZero()
				}
				if !set {
					missing(prefix + elem.Field(i).Tag.Get("yaml"))
				}
			}
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				name := strings.Split(v.Type().Field(i).Tag.Get("yaml"), ",")[0]
				walk(prefix+name+".", v.Field(i))
			}
		case reflect.Map:
			if v.Len() == 0 {
				missing(strings.TrimSuffix(prefix, "."))
			}
		default:
			if v.IsZero() {
				missing(strings.TrimSuffix(prefix, "."))
			}
		}
	}
	walk("", reflect.ValueOf(doc))
}
