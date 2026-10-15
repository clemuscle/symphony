package keycloak

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
)

func TestOIDCSettings(t *testing.T) {
	p, err := New("kc", provider.OIDCSettings{ClientID: "symphony"},
		Config{URL: "http://keycloak:8080/", PublicURL: "http://localhost:8180", Realm: "symphony"})
	if err != nil {
		t.Fatal(err)
	}
	s := p.OIDC()
	if s.Issuer != "http://localhost:8180/realms/symphony" || s.DiscoveryURL != "http://keycloak:8080/realms/symphony" {
		t.Errorf("issuer %s, découverte %s", s.Issuer, s.DiscoveryURL)
	}
	// Même URL pour Symphony et le navigateur : pas de découverte à part
	p, _ = New("kc", provider.OIDCSettings{ClientID: "symphony"}, Config{URL: "https://sso.example.com", Realm: "corp"})
	if s := p.OIDC(); s.Issuer != "https://sso.example.com/realms/corp" || s.DiscoveryURL != "" {
		t.Errorf("issuer %s, découverte %s", s.Issuer, s.DiscoveryURL)
	}
	if _, err := p.ListGroups(); !errors.Is(err, provider.ErrUnsupported) {
		t.Errorf("sans client d'annuaire : %v", err)
	}
	if _, err := New("kc", provider.OIDCSettings{ClientID: "s"}, Config{URL: "u", Realm: "r", DirectoryClientID: "d"}); err == nil {
		t.Error("client d'annuaire sans secret accepté")
	}
}

func TestListGroups(t *testing.T) {
	tokens, lists := 0, 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/realms/symphony/protocol/openid-connect/token":
			r.ParseForm()
			if r.Form.Get("grant_type") != "client_credentials" || r.Form.Get("client_secret") != "dir-secret" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			tokens++
			w.Write([]byte(`{"access_token":"admin-token","expires_in":300}`))
		case "/admin/realms/symphony/groups":
			if r.Header.Get("Authorization") != "Bearer admin-token" {
				w.WriteHeader(http.StatusUnauthorized)
				return
			}
			lists++
			// Keycloak 23+ : sous-groupes comptés, pas détaillés
			w.Write([]byte(`[
				{"id":"t1","name":"teams","path":"/teams","subGroupCount":1,"subGroups":[]},
				{"id":"a1","name":"symphony-admins","path":"/symphony-admins","subGroupCount":0,"subGroups":[]}
			]`))
		case "/admin/realms/symphony/groups/t1/children":
			w.Write([]byte(`[{"id":"t2","name":"team-payments","path":"/teams/team-payments","subGroupCount":0}]`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer srv.Close()

	p, err := New("kc", provider.OIDCSettings{ClientID: "symphony"}, Config{
		URL: srv.URL, Realm: "symphony", DirectoryClientID: "symphony-directory", DirectoryClientSecret: "dir-secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	groups, err := p.ListGroups()
	if err != nil {
		t.Fatal(err)
	}
	want := []provider.Group{
		{Name: "symphony-admins", Path: "/symphony-admins"},
		{Name: "teams", Path: "/teams"},
		{Name: "team-payments", Path: "/teams/team-payments"},
	}
	if len(groups) != len(want) {
		t.Fatalf("groupes = %+v", groups)
	}
	for i := range want {
		if groups[i] != want[i] {
			t.Errorf("groupe %d = %+v, attendu %+v", i, groups[i], want[i])
		}
	}
	p.ListGroups() // en cache
	if tokens != 1 || lists != 1 {
		t.Errorf("%d jetons, %d listes : attendu 1 et 1 (cache)", tokens, lists)
	}
}
