package handlers

import (
	"encoding/json"
	"errors"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/fake"
	"github.com/clemuscle/symphony/internal/provider/instrumented"
)

// stuckRuntime : un runtime dont la vérification ne répond pas
type stuckRuntime struct{ *fake.Runtime }

func (stuckRuntime) Check() (string, error) { time.Sleep(time.Second); return "", nil }

// brokenRegistry : un registry qui refuse son jeton
type brokenRegistry struct{ *fake.Registry }

func (brokenRegistry) Check() (string, error) { return "", errors.New("jeton refusé (401)") }

func TestCheckProviders(t *testing.T) {
	old := checkTimeout
	checkTimeout = 50 * time.Millisecond
	defer func() { checkTimeout = old }()

	forge := fake.NewForge(nil)
	r := provider.NewRegistry()
	r.RegisterSCM(forge.SCM(), true)
	r.RegisterRegistry(brokenRegistry{fake.NewRegistry()}, true)
	r.RegisterRuntime(stuckRuntime{fake.NewRuntime("lent", false)}, true)

	rec := httptest.NewRecorder()
	CheckProviders(r)(rec, httptest.NewRequest("GET", "/api/v1/admin/providers/check", nil))
	var got []providerCheck
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	byKind := map[string]providerCheck{}
	for _, c := range got {
		byKind[c.Kind] = c
	}
	if c := byKind["scm"]; !c.OK || c.Detail != "forge simulée" {
		t.Errorf("scm : %+v", c)
	}
	if c := byKind["registry"]; c.OK || c.Error != "jeton refusé (401)" {
		t.Errorf("registry : %+v", c)
	}
	if c := byKind["runtime"]; c.OK || c.Error != "pas de réponse après 50ms" {
		t.Errorf("runtime : %+v", c)
	}
}

func TestAdminStatus(t *testing.T) {
	forge := fake.NewForge(nil)
	r := provider.NewRegistry()
	r.RegisterSCM(instrumented.SCM(forge.SCM()), true)
	scm, _ := r.SCM("")
	scm.GetFileContent("platform/stacks", "main", "absent") // dernier appel : introuvable, pas une panne

	stacks := gitops.NewStackLoader(r, nil, gitops.StackSource{Repo: "platform/stacks", Branch: "main", Path: "stacks", Official: []string{"*"}})
	syncer := gitops.NewSyncer(r, catalogue.NewStore(), func() ([]gitops.ProjectRef, error) { return nil, nil }, "")
	status := func() config.Status {
		return config.Status{Source: "platform/config:symphony.yaml (branche main)", Revision: "abc",
			Types: map[string]string{"scm/gitlab": "gitlab"}}
	}

	rec := httptest.NewRecorder()
	AdminStatus(r, status, stacks, syncer)(rec, httptest.NewRequest("GET", "/api/v1/admin/status", nil))
	var got struct {
		Config    config.Status    `json:"config"`
		Providers []providerStatus `json:"providers"`
		Stacks    struct {
			Repo     string   `json:"repo"`
			Official []string `json:"official"`
			Loaded   []string `json:"loaded"`
			Refused  []any    `json:"refused"`
			Warnings []any    `json:"warnings"`
		} `json:"stacks"`
		Catalogue gitops.CatalogueReport `json:"catalogue"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.Config.Revision != "abc" || got.Stacks.Repo != "platform/stacks" || got.Stacks.Loaded == nil ||
		got.Stacks.Official[0] != "*" || got.Stacks.Refused == nil || got.Stacks.Warnings == nil ||
		got.Catalogue.File != "catalog-info.yaml" {
		t.Errorf("statut : %s", rec.Body)
	}
	if len(got.Providers) != 1 {
		t.Fatalf("providers : %+v", got.Providers)
	}
	p := got.Providers[0]
	if p.Kind != "scm" || p.Type != "gitlab" || !p.Default || p.LastCall == nil ||
		p.LastCall.Method != "GetFileContent" || p.LastCall.Error != "" {
		t.Errorf("provider : %+v (dernier appel %+v)", p, p.LastCall)
	}
}
