package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/webhook"
)

// fakeVaultKV : Vault minimal, KV v2 sur secret/, valeurs modifiables
func fakeVaultKV(t *testing.T, data map[string]string) *httptest.Server {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("X-Vault-Token") != "root" {
			w.WriteHeader(http.StatusForbidden)
			return
		}
		path := strings.TrimPrefix(r.URL.Path, "/v1/secret/data/")
		v, ok := data[path]
		if !ok {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Write([]byte(`{"data":{"data":` + v + `}}`))
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestBuildProvidersSecrets(t *testing.T) {
	vault := fakeVaultKV(t, map[string]string{
		"symphony/gitlab":  `{"token":"glpat-vault"}`,
		"symphony/webhook": `{"secret":"0123456789abcdef0123"}`,
	})
	t.Setenv("TEST_VAULT_TOKEN", "root")
	cfg, err := config.Parse([]byte(`
providers:
  secrets:
    - id: vault
      type: vault
      config:
        url: ` + vault.URL + `
        token: secret://env/TEST_VAULT_TOKEN
  scm:
    - id: gitlab
      type: gitlab
      config:
        url: http://gitlab
        token: secret://vault/symphony/gitlab#token
  ci:
    - id: ci
      type: gitlab_ci
      config:
        url: http://gitlab
        scm_ref: gitlab
webhooks:
  url: http://symphony:8080
  secret: secret://vault/symphony/webhook#secret
`))
	if err != nil {
		t.Fatal(err)
	}
	providers, resolved, err := buildProviders(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if got := providers.ListSecrets(); strings.Join(got, ",") != "env,vault" {
		t.Errorf("secrets = %v", got)
	}
	if resolved.Providers.SCM[0].Config["token"] != "glpat-vault" {
		t.Error("token du SCM non résolu")
	}
	// Le CI reprend le jeton du SCM scm_ref
	if got, _ := tokenOf(resolved.Providers.CI[0], resolved); got != "glpat-vault" {
		t.Errorf("jeton du CI = %q", got)
	}
	if s, err := buildWebhooks(resolved); err != nil || s.Secret != "0123456789abcdef0123" {
		t.Errorf("webhooks : %+v, %v", s, err)
	}
	// La configuration d'origine n'est pas modifiée (pas de secret en clair)
	if cfg.Providers.SCM[0].Config["token"] != "secret://vault/symphony/gitlab#token" {
		t.Error("configuration d'origine modifiée")
	}

	// Secret introuvable : configuration refusée
	cfg.Providers.SCM[0].Config["token"] = "secret://vault/symphony/absent#token"
	if _, _, err := buildProviders(cfg); err == nil || !strings.Contains(err.Error(), "secret://vault/symphony/absent#token") {
		t.Errorf("erreur = %v", err)
	}
	// Provider de secrets inconnu, id réservé
	cfg.Providers.SCM[0].Config["token"] = "secret://autre/x"
	if _, _, err := buildProviders(cfg); err == nil {
		t.Error("provider de secrets inconnu accepté")
	}
	cfg.Providers.SCM[0].Config["token"] = "secret://vault/symphony/gitlab#token"
	cfg.Providers.Secrets[0].ID = "env"
	if _, _, err := buildProviders(cfg); err == nil {
		t.Error("id env accepté pour un provider de secrets")
	}
}

func TestBuildPolicyIdentity(t *testing.T) {
	cfg, _ := config.Parse([]byte(`
providers:
  scm:
    - id: gitlab
      type: gitlab
      config:
        url: http://gitlab
        token: jeton
  identity:
    - id: keycloak
      type: keycloak
      config:
        url: http://keycloak:8080
        public_url: http://localhost:8180
        realm: symphony
        client_id: symphony
auth:
  identity: keycloak
  roles:
    viewer: [symphony-viewers]
`))
	providers, resolved, err := buildProviders(cfg)
	if err != nil {
		t.Fatal(err)
	}
	codec, _, _ := auth.NewCodec("")
	if p, err := buildPolicy(resolved, providers, codec); err != nil || p.Auth == nil {
		t.Errorf("politique : %+v, %v", p, err)
	}
	resolved.Auth.Identity = "absent"
	if _, err := buildPolicy(resolved, providers, codec); err == nil {
		t.Error("provider identity absent accepté")
	}
}

// TestConfigWatcherSecretRotation : un secret qui change dans son provider
// fait réappliquer la configuration, même si le fichier n'a pas changé
func TestConfigWatcherSecretRotation(t *testing.T) {
	data := map[string]string{"symphony/gitlab": `{"token":"v1"}`}
	vault := fakeVaultKV(t, data)
	content := []byte(`
providers:
  secrets:
    - id: vault
      type: vault
      config:
        url: ` + vault.URL + `
        token: root
  scm:
    - id: gitlab
      type: gitlab
      config:
        url: http://gitlab
        token: secret://vault/symphony/gitlab#token
`)
	source := &configSource{read: func() ([]byte, error) { return content, nil }}
	cfg, raw, err := source.load()
	if err != nil {
		t.Fatal(err)
	}
	providers, resolved, err := buildProviders(cfg)
	if err != nil {
		t.Fatal(err)
	}
	codec, _, _ := auth.NewCodec("")
	w := &configWatcher{
		source:      source,
		providers:   providers,
		syncer:      gitops.NewSyncer(providers, catalogue.NewStore(), func() ([]gitops.ProjectRef, error) { return nil, nil }, ""),
		stacks:      gitops.NewStackLoader(providers, nil, gitops.StackSource{Repo: "root/stacks", Branch: "main", Path: "stacks"}),
		security:    auth.NewHolder(&auth.Policy{}),
		webhooks:    webhook.NewHolder(nil),
		codec:       codec,
		last:        raw,
		fingerprint: fingerprint(resolved),
	}
	first := w.fingerprint

	w.check() // rien n'a changé
	if w.fingerprint != first {
		t.Error("configuration réappliquée sans changement")
	}
	data["symphony/gitlab"] = `{"token":"v2"}`
	w.check()
	if w.fingerprint == first {
		t.Error("rotation du secret non prise en compte")
	}
	// Vault ne répond plus pour ce secret : la configuration en place reste
	second := w.fingerprint
	delete(data, "symphony/gitlab")
	w.check()
	if w.fingerprint != second || w.secretErr == "" {
		t.Errorf("secret illisible : empreinte changée ou erreur non retenue (%q)", w.secretErr)
	}
}

// TestWebhookSecretRotationResubscribes : un nouveau secret des webhooks dans
// Vault fait réabonner les repos, avec ce secret
func TestWebhookSecretRotationResubscribes(t *testing.T) {
	data := map[string]string{"symphony/webhook": `{"secret":"premier-secret-1234"}`}
	vault := fakeVaultKV(t, data)
	content := []byte(`
providers:
  secrets:
    - id: vault
      type: vault
      config: {url: ` + vault.URL + `, token: root}
  scm:
    - id: gitlab
      type: gitlab
      config: {url: http://gitlab, token: t}
webhooks:
  url: http://symphony:8080
  secret: secret://vault/symphony/webhook#secret
`)
	source := &configSource{read: func() ([]byte, error) { return content, nil }}
	cfg, raw, err := source.load()
	if err != nil {
		t.Fatal(err)
	}
	providers, resolved, err := buildProviders(cfg)
	if err != nil {
		t.Fatal(err)
	}
	hooks, _ := buildWebhooks(resolved)
	codec, _, _ := auth.NewCodec("")
	got := make(chan string, 1)
	w := &configWatcher{
		source:      source,
		providers:   providers,
		syncer:      gitops.NewSyncer(providers, catalogue.NewStore(), func() ([]gitops.ProjectRef, error) { return nil, nil }, ""),
		stacks:      gitops.NewStackLoader(providers, nil, gitops.StackSource{Repo: "root/stacks", Branch: "main", Path: "stacks"}),
		security:    auth.NewHolder(&auth.Policy{}),
		webhooks:    webhook.NewHolder(hooks),
		codec:       codec,
		last:        raw,
		fingerprint: fingerprint(resolved),
		resubscribe: func(s *webhook.Settings) { got <- s.Secret },
	}
	data["symphony/webhook"] = `{"secret":"second-secret-5678"}`
	w.check()
	select {
	case s := <-got:
		if s != "second-secret-5678" {
			t.Errorf("réabonnement avec %q", s)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("pas de réabonnement après la rotation du secret")
	}
}
