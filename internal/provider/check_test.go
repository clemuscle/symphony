package provider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCheckStatus(t *testing.T) {
	if err := CheckStatus("gitlab", 200, nil); err != nil {
		t.Errorf("200 : %v", err)
	}
	for status, want := range map[int]string{
		401: "jeton refusé",
		403: "droits insuffisants",
		502: "status 502 — " + strings.Repeat("x", 200) + "…",
	} {
		err := CheckStatus("gitlab", status, []byte(strings.Repeat("x", 300)))
		if err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%d : %v, attendu %q", status, err, want)
		}
	}
}

func TestCheckOIDC(t *testing.T) {
	var issuer string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/realms/symphony/.well-known/openid-configuration" {
			http.NotFound(w, r)
			return
		}
		w.Write([]byte(`{"issuer":"` + issuer + `"}`))
	}))
	defer srv.Close()
	issuer = srv.URL + "/realms/symphony"

	if _, err := CheckOIDC(srv.Client(), OIDCSettings{Issuer: issuer}); err != nil {
		t.Errorf("issuer attendu : %v", err)
	}
	// Issuer annoncé différent : la vérification des jetons échouerait
	issuer = "http://keycloak.public/realms/symphony"
	if _, err := CheckOIDC(srv.Client(), OIDCSettings{Issuer: srv.URL + "/realms/symphony"}); err == nil ||
		!strings.Contains(err.Error(), "issuer annoncé") {
		t.Errorf("issuer différent : %v", err)
	}
	// Découverte lue ailleurs (URL interne) : l'issuer public est attendu tel quel
	if _, err := CheckOIDC(srv.Client(), OIDCSettings{Issuer: issuer, DiscoveryURL: srv.URL + "/realms/symphony"}); err != nil {
		t.Errorf("découverte interne : %v", err)
	}
	if _, err := CheckOIDC(srv.Client(), OIDCSettings{Issuer: srv.URL + "/realms/absent"}); err == nil ||
		!strings.Contains(err.Error(), "404") {
		t.Errorf("realm absent : %v", err)
	}
}
