package github

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
)

// TestPushFiles : un seul commit (arbre, commit, branche), quel que soit le
// nombre de fichiers
func TestPushFiles(t *testing.T) {
	var calls []string
	var tree struct {
		BaseTree string              `json:"base_tree"`
		Tree     []map[string]string `json:"tree"`
	}
	var commit struct {
		Parents []string `json:"parents"`
		Tree    string   `json:"tree"`
	}
	var ref map[string]string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/repos/me/api"))
		switch r.Method + " " + r.URL.Path {
		case "GET /repos/me/api/git/ref/heads/main":
			w.Write([]byte(`{"object":{"sha":"c0"}}`))
		case "GET /repos/me/api/git/commits/c0":
			w.Write([]byte(`{"tree":{"sha":"t0"}}`))
		case "POST /repos/me/api/git/trees":
			json.NewDecoder(r.Body).Decode(&tree)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"sha":"t1"}`))
		case "POST /repos/me/api/git/commits":
			json.NewDecoder(r.Body).Decode(&commit)
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{"sha":"c1"}`))
		case "PATCH /repos/me/api/git/refs/heads/main":
			json.NewDecoder(r.Body).Decode(&ref)
			w.Write([]byte(`{}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p := New("gh", "tok", "").WithAPIURL(srv.URL)
	err := p.PushFiles("me/api", "main", map[string]string{
		"Dockerfile":               "FROM scratch\n",
		".github/workflows/ci.yml": "on: push\n",
		"cmd/main.go":              "package main\n",
	}, "chore: scaffold")
	if err != nil {
		t.Fatal(err)
	}
	want := "GET /git/ref/heads/main,GET /git/commits/c0,POST /git/trees,POST /git/commits,PATCH /git/refs/heads/main"
	if strings.Join(calls, ",") != want {
		t.Errorf("appels :\n%s\nattendu :\n%s", strings.Join(calls, ","), want)
	}
	if tree.BaseTree != "t0" || len(tree.Tree) != 3 || tree.Tree[0]["path"] != ".github/workflows/ci.yml" {
		t.Errorf("arbre = %+v", tree)
	}
	if commit.Tree != "t1" || len(commit.Parents) != 1 || commit.Parents[0] != "c0" || ref["sha"] != "c1" {
		t.Errorf("commit = %+v, ref = %v", commit, ref)
	}
}

func TestCreateRepo(t *testing.T) {
	var path string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		json.NewDecoder(r.Body).Decode(&body)
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte(`{"full_name":"acme/api","name":"api","html_url":"https://github.com/acme/api","default_branch":"main"}`))
	}))
	defer srv.Close()

	p := New("gh", "tok", "").WithAPIURL(srv.URL)
	repo, err := p.CreateRepo(providerReq("api", "acme"))
	if err != nil {
		t.Fatal(err)
	}
	if path != "/orgs/acme/repos" || body["auto_init"] != true {
		t.Errorf("appel %s, body %v", path, body)
	}
	if repo.Path != "acme/api" || repo.DefaultBranch != "main" {
		t.Errorf("repo = %+v", repo)
	}
	p.CreateRepo(providerReq("api", ""))
	if path != "/user/repos" {
		t.Errorf("sans organisation : %s", path)
	}
}

func providerReq(name, ns string) provider.CreateRepoRequest {
	return provider.CreateRepoRequest{Name: name, Namespace: ns, Private: true}
}

func TestEnsureWebhook(t *testing.T) {
	var calls []string
	var body map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+r.URL.Path)
		switch r.Method {
		case "GET":
			w.Write([]byte(`[{"id":9,"config":{"url":"https://symphony/webhooks/github"}}]`))
		case "PATCH":
			json.NewDecoder(r.Body).Decode(&body)
			w.Write([]byte(`{}`))
		case "POST":
			w.WriteHeader(http.StatusCreated)
			w.Write([]byte(`{}`))
		}
	}))
	defer srv.Close()
	p := New("gh", "tok", "").WithAPIURL(srv.URL)
	if err := p.EnsureWebhook("me/api", "https://symphony/webhooks/github", "nouveau"); err != nil {
		t.Fatal(err)
	}
	if cfg, _ := body["config"].(map[string]any); cfg["secret"] != "nouveau" || cfg["content_type"] != "json" {
		t.Errorf("mise à jour : %v", body)
	}
	if err := p.EnsureWebhook("me/api", "https://ailleurs/webhooks/github", "s"); err != nil {
		t.Fatal(err)
	}
	if strings.Join(calls, ",") != "GET /repos/me/api/hooks,PATCH /repos/me/api/hooks/9,GET /repos/me/api/hooks,POST /repos/me/api/hooks" {
		t.Errorf("appels : %v", calls)
	}
}

// ListGroupRepos : une organisation, ou le compte du jeton (repos privés
// compris), sans archives ni forks
func TestListGroupRepos(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/orgs/acme":
			w.Write([]byte(`{"login":"acme"}`))
		case "/orgs/acme/repos":
			w.Write([]byte(`[{"full_name":"acme/api","default_branch":"main"},{"full_name":"acme/old","archived":true},{"full_name":"acme/fork","fork":true}]`))
		case "/user":
			w.Write([]byte(`{"login":"Me"}`))
		case "/user/repos":
			if r.URL.Query().Get("affiliation") != "owner" {
				t.Errorf("repos du compte : %s", r.URL.RawQuery)
			}
			w.Write([]byte(`[{"full_name":"me/private","default_branch":"main"}]`))
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()
	p := New("github", "t", "").WithAPIURL(srv.URL)
	repos, err := p.ListGroupRepos("acme")
	if err != nil || len(repos) != 1 || repos[0].Path != "acme/api" {
		t.Errorf("organisation : %+v %v", repos, err)
	}
	repos, err = p.ListGroupRepos("me")
	if err != nil || len(repos) != 1 || repos[0].Path != "me/private" {
		t.Errorf("compte du jeton : %+v %v", repos, err)
	}
	// Un autre compte : ses repos publics (/users/<nom>/repos), absent ici
	if _, err := p.ListGroupRepos("inconnu"); err == nil {
		t.Error("compte absent : erreur attendue")
	}
}
