package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"
	"strings"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type User struct {
	Sub     string   `json:"sub"`
	Email   string   `json:"email"`
	Name    string   `json:"name"`
	Groups  []string `json:"groups"`
	IsAdmin bool     `json:"is_admin"`
}

type ctxKey struct{}

func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxKey{}).(*User)
	return u, ok && u != nil
}

type Provider struct {
	verifier       *oidc.IDTokenVerifier
	oauth2         oauth2.Config
	adminEmails    map[string]bool
	deployerGroups map[string]bool // empty = all authenticated users
}

func New(ctx context.Context, cfg Config) (*Provider, error) {
	p, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	adminEmails := map[string]bool{}
	for _, e := range strings.Split(os.Getenv("ADMIN_EMAILS"), ",") {
		if e := strings.TrimSpace(e); e != "" {
			adminEmails[e] = true
		}
	}
	deployerGroups := map[string]bool{}
	for _, g := range strings.Split(os.Getenv("DEPLOYER_GROUPS"), ",") {
		if g := strings.TrimSpace(g); g != "" {
			deployerGroups[g] = true
		}
	}
	return &Provider{
		verifier: p.Verifier(&oidc.Config{ClientID: cfg.ClientID}),
		oauth2: oauth2.Config{
			ClientID:     cfg.ClientID,
			ClientSecret: cfg.ClientSecret,
			RedirectURL:  cfg.RedirectURL,
			Endpoint:     p.Endpoint(),
			Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
		},
		adminEmails:    adminEmails,
		deployerGroups: deployerGroups,
	}, nil
}

func (p *Provider) LoginHandler(w http.ResponseWriter, r *http.Request) {
	state := randomState()
	http.SetCookie(w, &http.Cookie{
		Name: "oauth_state", Value: state,
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 300,
	})
	http.Redirect(w, r, p.oauth2.AuthCodeURL(state), http.StatusFound)
}

func (p *Provider) CallbackHandler(w http.ResponseWriter, r *http.Request) {
	stateCookie, err := r.Cookie("oauth_state")
	if err != nil || stateCookie.Value != r.URL.Query().Get("state") {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", MaxAge: -1, Path: "/"})

	token, err := p.oauth2.Exchange(r.Context(), r.URL.Query().Get("code"))
	if err != nil {
		http.Error(w, "token exchange failed: "+err.Error(), http.StatusBadRequest)
		return
	}
	rawIDToken, ok := token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token in response", http.StatusBadRequest)
		return
	}
	if _, err := p.verifier.Verify(r.Context(), rawIDToken); err != nil {
		http.Error(w, "token verification failed", http.StatusBadRequest)
		return
	}
	http.SetCookie(w, &http.Cookie{
		Name: "symphony_token", Value: rawIDToken,
		Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode, MaxAge: 86400,
	})
	http.Redirect(w, r, frontendURL(), http.StatusFound)
}

func (p *Provider) LogoutHandler(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "symphony_token", MaxAge: -1, Path: "/"})
	http.Redirect(w, r, frontendURL()+"/login", http.StatusFound)
}

func (p *Provider) Middleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie("symphony_token")
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		idToken, err := p.verifier.Verify(r.Context(), cookie.Value)
		if err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		var claims struct {
			Sub               string   `json:"sub"`
			Email             string   `json:"email"`
			Name              string   `json:"name"`
			PreferredUsername string   `json:"preferred_username"`
			Groups            []string `json:"groups"`
		}
		if err := idToken.Claims(&claims); err != nil {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusUnauthorized)
			w.Write([]byte(`{"error":"unauthorized"}`))
			return
		}
		name := claims.Name
		if name == "" {
			name = claims.PreferredUsername
		}
		if name == "" {
			name = claims.Email
		}
		user := &User{
			Sub:     claims.Sub,
			Email:   claims.Email,
			Name:    name,
			Groups:  claims.Groups,
			IsAdmin: p.adminEmails[claims.Email],
		}
		ctx := context.WithValue(r.Context(), ctxKey{}, user)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (p *Provider) MeHandler(w http.ResponseWriter, r *http.Request) {
	user, _ := UserFromContext(r.Context())
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(user)
}

// CanDeploy returns true if the user is allowed to create projects, trigger
// pipelines and deployments. Admins always pass. If DEPLOYER_GROUPS is empty
// every authenticated user passes (permissive default for small teams).
func (p *Provider) CanDeploy(user *User) bool {
	if user.IsAdmin {
		return true
	}
	if len(p.deployerGroups) == 0 {
		return true
	}
	for _, g := range user.Groups {
		if p.deployerGroups[g] {
			return true
		}
	}
	return false
}

func randomState() string {
	b := make([]byte, 16)
	rand.Read(b)
	return base64.RawURLEncoding.EncodeToString(b)
}

func frontendURL() string {
	if u := os.Getenv("FRONTEND_URL"); u != "" {
		return u
	}
	return "http://localhost:5173"
}
