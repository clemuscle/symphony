package auth

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"os"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/yourorg/symphony/internal/rbac"
	"golang.org/x/oauth2"
)

type Config struct {
	Issuer       string
	ClientID     string
	ClientSecret string
	RedirectURL  string
}

type User struct {
	Sub     string    `json:"sub"`
	Email   string    `json:"email"`
	Name    string    `json:"name"`
	Groups  []string  `json:"groups"`
	Role    rbac.Role `json:"role"`
	IsAdmin bool      `json:"is_admin"` // derived from Role for backward compat
}

type ctxKey struct{}

func UserFromContext(ctx context.Context) (*User, bool) {
	u, ok := ctx.Value(ctxKey{}).(*User)
	return u, ok && u != nil
}

type Provider struct {
	verifier *oidc.IDTokenVerifier
	oauth2   oauth2.Config
	rbac     *rbac.Manager
}

func New(ctx context.Context, cfg Config, rbacMgr *rbac.Manager) (*Provider, error) {
	p, err := oidc.NewProvider(ctx, cfg.Issuer)
	if err != nil {
		return nil, err
	}
	if rbacMgr == nil {
		rbacMgr = rbac.Default()
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
		rbac: rbacMgr,
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
			jsonUnauthorized(w)
			return
		}
		idToken, err := p.verifier.Verify(r.Context(), cookie.Value)
		if err != nil {
			jsonUnauthorized(w)
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
			jsonUnauthorized(w)
			return
		}
		name := claims.Name
		if name == "" {
			name = claims.PreferredUsername
		}
		if name == "" {
			name = claims.Email
		}
		role := p.rbac.ResolveRole(claims.Groups)
		user := &User{
			Sub:     claims.Sub,
			Email:   claims.Email,
			Name:    name,
			Groups:  claims.Groups,
			Role:    role,
			IsAdmin: role == rbac.RoleAdmin,
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

func jsonUnauthorized(w http.ResponseWriter) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusUnauthorized)
	w.Write([]byte(`{"error":"unauthorized"}`))
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
