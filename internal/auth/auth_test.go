package auth

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
	"github.com/go-jose/go-jose/v4"
	"github.com/go-jose/go-jose/v4/jwt"
)

// fakeIdP : fournisseur OIDC minimal (découverte, JWKS, token) qui signe ses
// jetons avec une clé RSA générée pour le test
type fakeIdP struct {
	srv       *httptest.Server
	key       *rsa.PrivateKey
	challenge string // code_challenge reçu à l'autorisation (vérifié au token)
	nonce     string
	groups    []string
}

func newFakeIdP(t *testing.T) *fakeIdP {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	idp := &fakeIdP{key: key, groups: []string{"symphony-developers"}}
	mux := http.NewServeMux()
	idp.srv = httptest.NewServer(mux)
	t.Cleanup(idp.srv.Close)
	iss := idp.srv.URL

	mux.HandleFunc("/.well-known/openid-configuration", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(map[string]any{
			"issuer":                                iss,
			"authorization_endpoint":                iss + "/authorize",
			"token_endpoint":                        iss + "/token",
			"jwks_uri":                              iss + "/jwks",
			"end_session_endpoint":                  iss + "/logout",
			"id_token_signing_alg_values_supported": []string{"RS256"},
		})
	})
	mux.HandleFunc("/jwks", func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(jose.JSONWebKeySet{Keys: []jose.JSONWebKey{
			{Key: &key.PublicKey, KeyID: "k1", Algorithm: "RS256", Use: "sig"},
		}})
	})
	mux.HandleFunc("/token", func(w http.ResponseWriter, r *http.Request) {
		r.ParseForm()
		// PKCE : S256(code_verifier) doit valoir le code_challenge reçu
		sum := sha256.Sum256([]byte(r.Form.Get("code_verifier")))
		if base64.RawURLEncoding.EncodeToString(sum[:]) != idp.challenge || r.Form.Get("code") != "code-ok" {
			http.Error(w, `{"error":"invalid_grant"}`, http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]any{
			"access_token": "opaque", "token_type": "Bearer", "expires_in": 300,
			"id_token": idp.sign(t, "symphony", idp.nonce),
		})
	})
	return idp
}

// sign émet un jeton pour l'audience aud
func (idp *fakeIdP) sign(t *testing.T, aud, nonce string) string {
	signer, err := jose.NewSigner(jose.SigningKey{Algorithm: jose.RS256, Key: idp.key},
		(&jose.SignerOptions{}).WithHeader("kid", "k1"))
	if err != nil {
		t.Fatal(err)
	}
	claims := map[string]any{
		"iss": idp.srv.URL, "sub": "u-1", "aud": aud,
		"exp": time.Now().Add(time.Hour).Unix(), "iat": time.Now().Unix(),
		"name": "Alice", "email": "alice@example.com",
		"groups": idp.groups,
	}
	if nonce != "" {
		claims["nonce"] = nonce
	}
	raw, err := jwt.Signed(signer).Claims(claims).Serialize()
	if err != nil {
		t.Fatal(err)
	}
	return raw
}

func newTestHolder(t *testing.T, idp *fakeIdP) *Holder {
	codec, _, _ := NewCodec(strings.Repeat("s", 32))
	a, err := NewAuthenticator(provider.OIDCSettings{
		Issuer: idp.srv.URL, ClientID: "symphony",
	}, map[string][]string{
		"admin":     {"symphony-admins"},
		"developer": {"symphony-developers"},
		"viewer":    {"symphony-viewers"},
	}, codec)
	if err != nil {
		t.Fatal(err)
	}
	return NewHolder(&Policy{Auth: a})
}

// protected : route protégée par Authenticate puis Require(min), qui renvoie l'utilisateur
func protected(h *Holder, min Role) http.Handler {
	return h.Authenticate(Require(min)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		json.NewEncoder(w).Encode(UserFrom(r.Context()))
	})))
}

func TestLoginFlow(t *testing.T) {
	idp := newFakeIdP(t)
	h := newTestHolder(t, idp)

	// 1. /auth/login redirige vers l'IdP, avec PKCE S256 et un nonce
	rec := httptest.NewRecorder()
	h.Login(rec, httptest.NewRequest("GET", "http://symphony.test/auth/login?redirect=/projects", nil))
	if rec.Code != http.StatusFound {
		t.Fatalf("login : %d %s", rec.Code, rec.Body)
	}
	authURL, _ := url.Parse(rec.Header().Get("Location"))
	q := authURL.Query()
	if q.Get("code_challenge_method") != "S256" || q.Get("redirect_uri") != "http://symphony.test/auth/callback" {
		t.Fatalf("URL d'autorisation : %s", authURL)
	}
	idp.challenge, idp.nonce = q.Get("code_challenge"), q.Get("nonce")
	loginC := rec.Result().Cookies()[0]

	// 2. Retour de l'IdP : state inattendu refusé
	bad := httptest.NewRequest("GET", "http://symphony.test/auth/callback?code=code-ok&state=autre", nil)
	bad.AddCookie(loginC)
	rec = httptest.NewRecorder()
	h.Callback(rec, bad)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("state inattendu : %d", rec.Code)
	}

	// 3. Retour correct : session posée, redirection vers la page demandée
	cb := httptest.NewRequest("GET", "http://symphony.test/auth/callback?code=code-ok&state="+q.Get("state"), nil)
	cb.AddCookie(loginC)
	rec = httptest.NewRecorder()
	h.Callback(rec, cb)
	if rec.Code != http.StatusFound || rec.Header().Get("Location") != "/projects" {
		t.Fatalf("callback : %d %s %s", rec.Code, rec.Header().Get("Location"), rec.Body)
	}
	var sessionC, logoutC *http.Cookie
	for _, c := range rec.Result().Cookies() {
		switch c.Name {
		case sessionCookie:
			sessionC = c
		case logoutCookie:
			logoutC = c
		}
	}
	if logoutC == nil || logoutC.Path != "/auth/logout" || !logoutC.HttpOnly {
		t.Fatalf("cookie de déconnexion : %+v", logoutC)
	}
	if sessionC == nil || !sessionC.HttpOnly || sessionC.SameSite != http.SameSiteLaxMode {
		t.Fatalf("cookie de session : %+v", sessionC)
	}
	if strings.Contains(sessionC.Value, "opaque") {
		t.Error("le cookie ne doit pas contenir de jeton")
	}

	// 4. Le cookie ouvre l'API avec le rôle déduit des groupes
	req := httptest.NewRequest("GET", "http://symphony.test/api/v1/projects", nil)
	req.AddCookie(sessionC)
	rec = httptest.NewRecorder()
	protected(h, RoleDeveloper).ServeHTTP(rec, req)
	var u User
	json.NewDecoder(rec.Body).Decode(&u)
	if rec.Code != 200 || u.Email != "alice@example.com" || u.Role != RoleDeveloper {
		t.Errorf("API avec session : %d %+v", rec.Code, u)
	}

	// 5. Rôle insuffisant
	rec = httptest.NewRecorder()
	protected(h, RoleAdmin).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("rôle admin exigé : %d", rec.Code)
	}

	// 6. CSRF : POST avec le cookie depuis un autre site refusé, depuis Symphony accepté
	for origin, want := range map[string]int{"http://evil.test": 403, "http://symphony.test": 200} {
		post := httptest.NewRequest("POST", "http://symphony.test/api/v1/projects", nil)
		post.AddCookie(sessionC)
		post.Header.Set("Origin", origin)
		rec = httptest.NewRecorder()
		protected(h, RoleDeveloper).ServeHTTP(rec, post)
		if rec.Code != want {
			t.Errorf("POST depuis %s : %d, attendu %d", origin, rec.Code, want)
		}
	}

	// Déconnexion : session effacée, retour à l'IdP avec id_token_hint
	out := httptest.NewRequest("POST", "http://symphony.test/auth/logout", nil)
	out.AddCookie(logoutC)
	rec = httptest.NewRecorder()
	h.Logout(rec, out)
	loc, _ := url.Parse(rec.Header().Get("Location"))
	if rec.Code != http.StatusSeeOther || loc.Path != "/logout" || loc.Query().Get("id_token_hint") != logoutC.Value {
		t.Errorf("logout : %d %s", rec.Code, loc)
	}

	// 7. Cookie falsifié
	forged := httptest.NewRequest("GET", "http://symphony.test/api/v1/projects", nil)
	forged.AddCookie(&http.Cookie{Name: sessionCookie, Value: sessionC.Value + "x"})
	rec = httptest.NewRecorder()
	protected(h, RoleViewer).ServeHTTP(rec, forged)
	if rec.Code != http.StatusUnauthorized {
		t.Errorf("cookie falsifié : %d", rec.Code)
	}
}

func TestBearer(t *testing.T) {
	idp := newFakeIdP(t)
	h := newTestHolder(t, idp)
	call := func(token string, origin string) int {
		req := httptest.NewRequest("POST", "http://symphony.test/api/v1/projects", nil)
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		if origin != "" {
			req.Header.Set("Origin", origin)
		}
		rec := httptest.NewRecorder()
		protected(h, RoleDeveloper).ServeHTTP(rec, req)
		return rec.Code
	}
	if c := call(idp.sign(t, "symphony", ""), "http://ailleurs.test"); c != 200 {
		t.Errorf("Bearer valide (pas de contrôle d'origine sans cookie) : %d", c)
	}
	if c := call(idp.sign(t, "autre-client", ""), ""); c != 401 {
		t.Errorf("Bearer d'une autre audience : %d", c)
	}
	if c := call("pas-un-jwt", ""); c != 401 {
		t.Errorf("Bearer invalide : %d", c)
	}
	if c := call("", ""); c != 401 {
		t.Errorf("sans authentification : %d", c)
	}

	// Utilisateur sans groupe connu : authentifié, mais aucun rôle
	idp.groups = []string{"marketing"}
	if c := call(idp.sign(t, "symphony", ""), ""); c != 403 {
		t.Errorf("sans rôle : %d", c)
	}
}

func TestDisabled(t *testing.T) {
	h := NewHolder(&Policy{})
	rec := httptest.NewRecorder()
	protected(h, RoleAdmin).ServeHTTP(rec, httptest.NewRequest("DELETE", "/api/v1/projects/x", nil))
	if rec.Code != 200 {
		t.Errorf("authentification désactivée : %d", rec.Code)
	}
	// Même sans authentification, une page d'un autre site ne modifie rien
	req := httptest.NewRequest("POST", "http://localhost:8080/api/v1/sync", nil)
	req.Header.Set("Origin", "http://evil.test")
	rec = httptest.NewRecorder()
	protected(h, RoleAdmin).ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Errorf("POST d'un autre site, auth désactivée : %d", rec.Code)
	}
}

func TestCORS(t *testing.T) {
	h := NewHolder(&Policy{CORSOrigins: []string{"https://portail.example.com"}})
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	for origin, allowed := range map[string]bool{"https://portail.example.com": true, "https://evil.test": false} {
		req := httptest.NewRequest("GET", "/api/v1/services", nil)
		req.Header.Set("Origin", origin)
		rec := httptest.NewRecorder()
		h.CORS(next).ServeHTTP(rec, req)
		if got := rec.Header().Get("Access-Control-Allow-Origin") == origin; got != allowed {
			t.Errorf("%s : autorisée = %v", origin, got)
		}
	}
}

func TestRoleMapper(t *testing.T) {
	m, err := NewRoleMapper(map[string][]string{"viewer": {"tous"}, "admin": {"ops"}})
	if err != nil {
		t.Fatal(err)
	}
	if r := m.Resolve([]string{"tous", "ops"}); r != RoleAdmin {
		t.Errorf("rôle le plus élevé : %s", r)
	}
	if r := m.Resolve([]string{"autre"}); r != RoleNone {
		t.Errorf("groupe inconnu : %s", r)
	}
	if _, err := NewRoleMapper(map[string][]string{"superuser": {"x"}}); err == nil {
		t.Error("rôle inconnu accepté")
	}
}

func TestCodec(t *testing.T) {
	c, _, _ := NewCodec(strings.Repeat("k", 32))
	v, _ := c.Encode(session{Subject: "s"}, time.Minute)
	var s session
	if err := c.Decode(v, &s); err != nil || s.Subject != "s" {
		t.Errorf("aller-retour : %v %+v", err, s)
	}
	other, _, _ := NewCodec(strings.Repeat("z", 32))
	if other.Decode(v, &s) == nil {
		t.Error("signature d'une autre clé acceptée")
	}
	expired, _ := c.Encode(session{}, -time.Second)
	if c.Decode(expired, &s) == nil {
		t.Error("cookie expiré accepté")
	}
	if _, _, err := NewCodec("court"); err == nil {
		t.Error("secret trop court accepté")
	}
}

func TestSafeRedirect(t *testing.T) {
	for in, want := range map[string]string{
		"/projects": "/projects", "": "/", "https://evil.test": "/", "//evil.test": "/", "/\\evil.test": "/",
	} {
		if got := safeRedirect(in); got != want {
			t.Errorf("safeRedirect(%q) = %q", in, got)
		}
	}
}
