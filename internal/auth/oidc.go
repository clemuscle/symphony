package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

const (
	sessionCookie = "symphony_session"
	loginCookie   = "symphony_login"
	// ID token gardé pour la seule déconnexion (id_token_hint, sinon Keycloak
	// demande une confirmation) : cookie limité à /auth/logout, jamais envoyé à l'API
	logoutCookie = "symphony_logout"
	sessionTTL   = 8 * time.Hour
	loginTTL     = 10 * time.Minute
)

// Authenticator fait le flux OIDC (Authorization Code + PKCE) côté serveur et
// reconnaît les utilisateurs : cookie de session ou jeton Bearer.
type Authenticator struct {
	cfg   provider.OIDCSettings
	roles *RoleMapper
	codec *Codec

	// Découverte OIDC faite au premier besoin, et retentée tant qu'elle
	// échoue : Symphony démarre même si le fournisseur d'identité est absent
	mu         sync.Mutex
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	endSession string
}

// session : contenu du cookie de session (pas de jeton)
type session struct {
	Subject string   `json:"sub"`
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Groups  []string `json:"groups"`
}

// loginState : contenu du cookie posé le temps de l'aller-retour chez l'IdP
type loginState struct {
	State    string `json:"state"`
	Verifier string `json:"verifier"`
	Nonce    string `json:"nonce"`
	Redirect string `json:"redirect"`
}

// NewAuthenticator valide les réglages OIDC du provider d'identité et les
// rôles (rôle → groupes) ; la découverte OIDC viendra plus tard
func NewAuthenticator(cfg provider.OIDCSettings, roleGroups map[string][]string, codec *Codec) (*Authenticator, error) {
	if cfg.Issuer == "" || cfg.ClientID == "" {
		return nil, errors.New("issuer et client_id requis")
	}
	if len(roleGroups) == 0 {
		return nil, errors.New("auth.roles requis : personne n'aurait de droits")
	}
	roles, err := NewRoleMapper(roleGroups)
	if err != nil {
		return nil, fmt.Errorf("auth.roles: %w", err)
	}
	if cfg.GroupsClaim == "" {
		cfg.GroupsClaim = "groups"
	}
	return &Authenticator{cfg: cfg, roles: roles, codec: codec}, nil
}

func (a *Authenticator) discover(ctx context.Context) (*oidc.Provider, *oidc.IDTokenVerifier, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.provider != nil {
		return a.provider, a.verifier, nil
	}
	discovery := a.cfg.Issuer
	if a.cfg.DiscoveryURL != "" {
		// Découverte lue ailleurs, mais l'issuer attendu reste celui du navigateur
		ctx = oidc.InsecureIssuerURLContext(ctx, a.cfg.Issuer)
		discovery = a.cfg.DiscoveryURL
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	p, err := oidc.NewProvider(ctx, discovery)
	if err != nil {
		return nil, nil, fmt.Errorf("découverte OIDC %s: %w", discovery, err)
	}
	var meta struct {
		EndSession string `json:"end_session_endpoint"`
	}
	p.Claims(&meta)
	a.provider = p
	a.verifier = p.Verifier(&oidc.Config{ClientID: a.cfg.ClientID})
	a.endSession = meta.EndSession
	return a.provider, a.verifier, nil
}

func (a *Authenticator) oauth2Config(r *http.Request, p *oidc.Provider) *oauth2.Config {
	return &oauth2.Config{
		ClientID:     a.cfg.ClientID,
		ClientSecret: a.cfg.ClientSecret,
		RedirectURL:  a.baseURL(r) + "/auth/callback",
		Endpoint:     p.Endpoint(),
		Scopes:       []string{oidc.ScopeOpenID, "profile", "email"},
	}
}

// baseURL : origine publique de Symphony (redirect_url configurée, sinon la requête)
func (a *Authenticator) baseURL(r *http.Request) string {
	if a.cfg.RedirectURL != "" {
		return strings.TrimSuffix(strings.TrimSuffix(a.cfg.RedirectURL, "/"), "/auth/callback")
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return scheme + "://" + r.Host
}

func (a *Authenticator) secure(r *http.Request) bool {
	return strings.HasPrefix(a.baseURL(r), "https://")
}

func (a *Authenticator) setCookie(w http.ResponseWriter, r *http.Request, name, value, path string, ttl time.Duration) {
	http.SetCookie(w, &http.Cookie{
		Name: name, Value: value, Path: path, MaxAge: int(ttl.Seconds()),
		HttpOnly: true, Secure: a.secure(r), SameSite: http.SameSiteLaxMode,
	})
}

// Login redirige vers le fournisseur d'identité
func (a *Authenticator) Login(w http.ResponseWriter, r *http.Request) {
	p, _, err := a.discover(r.Context())
	if err != nil {
		slog.Error("connexion impossible", "error", err)
		http.Error(w, "Fournisseur d'identité injoignable, réessayer plus tard.", http.StatusServiceUnavailable)
		return
	}
	ls := loginState{
		State:    randomString(),
		Verifier: oauth2.GenerateVerifier(),
		Nonce:    randomString(),
		Redirect: safeRedirect(r.URL.Query().Get("redirect")),
	}
	value, err := a.codec.Encode(ls, loginTTL)
	if err != nil {
		slog.Error("connexion : état de connexion impossible à signer", "error", err)
		http.Error(w, "Erreur interne, réessayer plus tard.", http.StatusInternalServerError)
		return
	}
	a.setCookie(w, r, loginCookie, value, "/auth/", loginTTL)
	http.Redirect(w, r, a.oauth2Config(r, p).AuthCodeURL(ls.State,
		oauth2.S256ChallengeOption(ls.Verifier), oidc.Nonce(ls.Nonce)), http.StatusFound)
}

// Callback termine la connexion : échange du code, vérification de l'ID token,
// pose du cookie de session
func (a *Authenticator) Callback(w http.ResponseWriter, r *http.Request) {
	fail := func(msg string, err error) {
		slog.Warn("connexion refusée", "raison", msg, "error", err)
		http.Error(w, "Connexion refusée : "+msg+". Recommencer depuis l'accueil.", http.StatusUnauthorized)
	}
	if e := r.URL.Query().Get("error"); e != "" {
		fail("le fournisseur d'identité a répondu "+e, nil)
		return
	}
	c, err := r.Cookie(loginCookie)
	if err != nil {
		fail("connexion non initiée ou expirée", err)
		return
	}
	var ls loginState
	if err := a.codec.Decode(c.Value, &ls); err != nil {
		fail("état de connexion invalide", err)
		return
	}
	if r.URL.Query().Get("state") != ls.State {
		fail("paramètre state inattendu", nil)
		return
	}
	p, verifier, err := a.discover(r.Context())
	if err != nil {
		fail("fournisseur d'identité injoignable", err)
		return
	}
	tok, err := a.oauth2Config(r, p).Exchange(r.Context(), r.URL.Query().Get("code"), oauth2.VerifierOption(ls.Verifier))
	if err != nil {
		fail("échange du code impossible", err)
		return
	}
	rawID, _ := tok.Extra("id_token").(string)
	idt, err := verifier.Verify(r.Context(), rawID)
	if err != nil {
		fail("ID token invalide", err)
		return
	}
	if idt.Nonce != ls.Nonce {
		fail("nonce inattendu", nil)
		return
	}
	s, err := a.sessionFrom(idt)
	if err != nil {
		fail("claims illisibles", err)
		return
	}
	value, err := a.codec.Encode(s, sessionTTL)
	if err != nil {
		fail("session", err)
		return
	}
	a.setCookie(w, r, sessionCookie, value, "/", sessionTTL)
	a.setCookie(w, r, logoutCookie, rawID, "/auth/logout", sessionTTL)
	a.setCookie(w, r, loginCookie, "", "/auth/", -time.Second)
	slog.Info("connexion", "user", s.Email, "groups", s.Groups)
	http.Redirect(w, r, ls.Redirect, http.StatusFound)
}

// Logout efface la session puis termine celle du fournisseur d'identité
func (a *Authenticator) Logout(w http.ResponseWriter, r *http.Request) {
	clearSession(w, a.secure(r))
	target := "/"
	if _, _, err := a.discover(r.Context()); err == nil && a.endSession != "" {
		q := url.Values{
			"client_id":                {a.cfg.ClientID},
			"post_logout_redirect_uri": {a.baseURL(r) + "/"},
		}
		if c, err := r.Cookie(logoutCookie); err == nil && c.Value != "" {
			q.Set("id_token_hint", c.Value)
		}
		target = a.endSession + "?" + q.Encode()
	}
	a.setCookie(w, r, logoutCookie, "", "/auth/logout", -time.Second)
	http.Redirect(w, r, target, http.StatusSeeOther)
}

func clearSession(w http.ResponseWriter, secure bool) {
	http.SetCookie(w, &http.Cookie{Name: sessionCookie, Value: "", Path: "/", MaxAge: -1,
		HttpOnly: true, Secure: secure, SameSite: http.SameSiteLaxMode})
}

func (a *Authenticator) sessionFrom(idt *oidc.IDToken) (session, error) {
	var claims map[string]any
	if err := idt.Claims(&claims); err != nil {
		return session{}, err
	}
	str := func(k string) string { s, _ := claims[k].(string); return s }
	name := str("name")
	if name == "" {
		name = str("preferred_username")
	}
	var groups []string
	if list, ok := claims[a.cfg.GroupsClaim].([]any); ok {
		for _, g := range list {
			if s, ok := g.(string); ok {
				groups = append(groups, strings.TrimPrefix(s, "/")) // Keycloak : chemin complet possible
			}
		}
	}
	return session{Subject: idt.Subject, Name: name, Email: str("email"), Groups: sortedGroups(groups)}, nil
}

// authenticate reconnaît l'utilisateur : jeton Bearer (scripts, CI), sinon
// cookie de session. viaCookie sert au contrôle d'origine (CSRF).
func (a *Authenticator) authenticate(r *http.Request) (u *User, viaCookie bool, err error) {
	var s session
	if h := r.Header.Get("Authorization"); strings.HasPrefix(h, "Bearer ") {
		_, verifier, err := a.discover(r.Context())
		if err != nil {
			return nil, false, err
		}
		tok, err := verifier.Verify(r.Context(), strings.TrimPrefix(h, "Bearer "))
		if err != nil {
			return nil, false, fmt.Errorf("jeton invalide: %w", err)
		}
		if s, err = a.sessionFrom(tok); err != nil {
			return nil, false, err
		}
	} else {
		c, err := r.Cookie(sessionCookie)
		if err != nil {
			return nil, false, errors.New("non connecté")
		}
		if err := a.codec.Decode(c.Value, &s); err != nil {
			return nil, false, err
		}
		viaCookie = true
	}
	return &User{
		Subject: s.Subject, Name: s.Name, Email: s.Email, Groups: s.Groups,
		Role: a.roles.Resolve(s.Groups), // recalculé à chaque requête : suit la configuration
	}, viaCookie, nil
}

// safeRedirect n'accepte qu'un chemin local (pas de redirection ouverte)
func safeRedirect(target string) string {
	if !strings.HasPrefix(target, "/") || strings.HasPrefix(target, "//") || strings.HasPrefix(target, "/\\") {
		return "/"
	}
	return target
}
