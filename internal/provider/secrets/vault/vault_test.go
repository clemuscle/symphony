package vault

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
)

// fakeVault : KV v2 sur secret/, KV v1 sur kv/, AppRole ; tokens acceptés
// dans valid (un login AppRole y ajoute le sien)
func fakeVault(t *testing.T) (*httptest.Server, *int, map[string]bool) {
	t.Helper()
	logins := 0
	valid := map[string]bool{"root": true}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/v1/auth/approle/login" {
			var body map[string]string
			json.NewDecoder(r.Body).Decode(&body)
			if body["role_id"] != "role" || body["secret_id"] != "sid" {
				w.WriteHeader(http.StatusBadRequest)
				w.Write([]byte(`{"errors":["invalid role or secret ID"]}`))
				return
			}
			logins++
			tok := "approle-" + string(rune('0'+logins))
			valid[tok] = true
			json.NewEncoder(w).Encode(map[string]any{"auth": map[string]any{"client_token": tok, "lease_duration": 3600}})
			return
		}
		if !valid[r.Header.Get("X-Vault-Token")] {
			w.WriteHeader(http.StatusForbidden)
			w.Write([]byte(`{"errors":["permission denied"]}`))
			return
		}
		switch r.URL.Path {
		case "/v1/auth/token/lookup-self":
			w.Write([]byte(`{"data":{"display_name":"approle"}}`))
		case "/v1/secret/data/symphony/gitlab":
			w.Write([]byte(`{"data":{"data":{"token":"glpat-123","user":"root"},"metadata":{"version":2}}}`))
		case "/v1/secret/data/symphony/argocd":
			w.Write([]byte(`{"data":{"data":{"token":"argo-456"}}}`))
		case "/v1/kv/symphony/webhook":
			w.Write([]byte(`{"data":{"secret":"w-789"}}`))
		default:
			w.WriteHeader(http.StatusNotFound)
			w.Write([]byte(`{"errors":[]}`))
		}
	}))
	t.Cleanup(srv.Close)
	return srv, &logins, valid
}

func TestGetToken(t *testing.T) {
	srv, _, _ := fakeVault(t)
	p, err := New("vault", Config{URL: srv.URL, Token: "root"})
	if err != nil {
		t.Fatal(err)
	}
	if v, err := p.Get("symphony/gitlab", "token"); err != nil || v != "glpat-123" {
		t.Errorf("Get = %q, %v", v, err)
	}
	// Une seule clé : elle est prise par défaut
	if v, err := p.Get("symphony/argocd", ""); err != nil || v != "argo-456" {
		t.Errorf("clé par défaut = %q, %v", v, err)
	}
	if _, err := p.Get("symphony/gitlab", ""); err == nil || !strings.Contains(err.Error(), "#token") {
		t.Errorf("plusieurs clés sans précision : %v", err)
	}
	if _, err := p.Get("symphony/gitlab", "absente"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("clé absente : %v", err)
	}
	if _, err := p.Get("symphony/absent", "x"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("secret absent : %v", err)
	}
}

func TestGetKV1(t *testing.T) {
	srv, _, _ := fakeVault(t)
	p, _ := New("vault", Config{URL: srv.URL, Token: "root", Mount: "kv", KVVersion: 1})
	if v, err := p.Get("symphony/webhook", "secret"); err != nil || v != "w-789" {
		t.Errorf("Get = %q, %v", v, err)
	}
}

func TestAppRole(t *testing.T) {
	srv, logins, valid := fakeVault(t)
	p, _ := New("vault", Config{URL: srv.URL, RoleID: "role", SecretID: "sid"})
	for range 2 {
		if v, err := p.Get("symphony/gitlab", "token"); err != nil || v != "glpat-123" {
			t.Fatalf("Get = %q, %v", v, err)
		}
	}
	if *logins != 1 {
		t.Errorf("%d connexions AppRole, attendu 1 (jeton réutilisé)", *logins)
	}
	// Jeton révoqué : nouvelle connexion, une seule fois
	delete(valid, "approle-1")
	if _, err := p.Get("symphony/gitlab", "token"); err != nil {
		t.Fatalf("après révocation : %v", err)
	}
	if *logins != 2 {
		t.Errorf("%d connexions AppRole, attendu 2", *logins)
	}

	bad, _ := New("vault", Config{URL: srv.URL, RoleID: "role", SecretID: "faux"})
	if _, err := bad.Get("symphony/gitlab", "token"); err == nil || !strings.Contains(err.Error(), "invalid role or secret ID") {
		t.Errorf("AppRole refusé : %v", err)
	}
}

func TestNewValidation(t *testing.T) {
	if _, err := New("vault", Config{Token: "x"}); err == nil {
		t.Error("url manquante acceptée")
	}
	if _, err := New("vault", Config{URL: "http://v"}); err == nil {
		t.Error("sans jeton ni AppRole accepté")
	}
	if _, err := New("vault", Config{URL: "http://v", Token: "x", KVVersion: 3}); err == nil {
		t.Error("kv_version 3 acceptée")
	}
}

func TestCheck(t *testing.T) {
	srv, _, _ := fakeVault(t)
	p, err := New("vault", Config{URL: srv.URL, RoleID: "role", SecretID: "sid"})
	if err != nil {
		t.Fatal(err)
	}
	if detail, err := p.Check(); err != nil || !strings.Contains(detail, "approle") {
		t.Errorf("Check = %q, %v", detail, err)
	}
	p, _ = New("vault", Config{URL: srv.URL, Token: "révoqué"})
	if _, err := p.Check(); err == nil || !strings.Contains(err.Error(), "permission denied") {
		t.Errorf("jeton refusé attendu, obtenu %v", err)
	}
}
