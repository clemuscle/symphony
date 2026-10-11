package auth

import (
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
)

// Policy : règles de sécurité HTTP issues de la configuration. Elle est
// remplacée d'un bloc au rechargement de la configuration (Holder.Set).
type Policy struct {
	Auth        *Authenticator // nil : authentification désactivée
	CORSOrigins []string
}

// Holder porte la Policy courante ; routes et middlewares passent par lui
type Holder struct{ p atomic.Pointer[Policy] }

func NewHolder(p *Policy) *Holder {
	h := &Holder{}
	h.Set(p)
	return h
}

func (h *Holder) Set(p *Policy) { h.p.Store(p) }
func (h *Holder) get() *Policy  { return h.p.Load() }

// Enabled indique si l'authentification est active
func (h *Holder) Enabled() bool { return h.get().Auth != nil }

func jsonError(w http.ResponseWriter, code int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	json.NewEncoder(w).Encode(map[string]string{"error": msg})
}

// Authenticate exige un utilisateur reconnu et le place dans le contexte.
// Authentification désactivée : utilisateur « anonyme » avec le rôle admin.
func (h *Holder) Authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := h.get().Auth
		if a == nil {
			next.ServeHTTP(w, r.WithContext(withUser(r.Context(), anonymous)))
			return
		}
		u, viaCookie, err := a.authenticate(r)
		if err != nil {
			jsonError(w, http.StatusUnauthorized, "authentification requise")
			return
		}
		// Le cookie part avec toute requête vers Symphony : une requête qui
		// modifie quelque chose doit venir de Symphony lui-même (CSRF)
		if viaCookie && !safeMethod(r.Method) && !sameOrigin(r) {
			jsonError(w, http.StatusForbidden, "origine de la requête refusée")
			return
		}
		next.ServeHTTP(w, r.WithContext(withUser(r.Context(), u)))
	})
}

// Require exige au moins le rôle min (après Authenticate)
func Require(min Role) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u := UserFrom(r.Context())
			if u == nil || !u.Role.AtLeast(min) {
				jsonError(w, http.StatusForbidden, "rôle "+string(min)+" requis")
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Me renvoie l'utilisateur courant (le front s'en sert pour adapter l'UI)
func (h *Holder) Me(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"auth_enabled": h.Enabled(),
		"user":         UserFrom(r.Context()),
	})
}

// Login, Callback et Logout : routes /auth/*
func (h *Holder) Login(w http.ResponseWriter, r *http.Request) {
	if a := h.get().Auth; a != nil {
		a.Login(w, r)
		return
	}
	http.Redirect(w, r, safeRedirect(r.URL.Query().Get("redirect")), http.StatusFound)
}

func (h *Holder) Callback(w http.ResponseWriter, r *http.Request) {
	if a := h.get().Auth; a != nil {
		a.Callback(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusFound)
}

func (h *Holder) Logout(w http.ResponseWriter, r *http.Request) {
	if a := h.get().Auth; a != nil {
		a.Logout(w, r)
		return
	}
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

// CORS n'autorise que les origines configurées, sans cookies : un client
// d'une autre origine s'authentifie avec un jeton Bearer
func (h *Holder) CORS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		origin := r.Header.Get("Origin")
		if origin != "" && h.allowedOrigin(origin) {
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, DELETE, OPTIONS")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Request-ID")
			w.Header().Add("Vary", "Origin")
			if r.Method == http.MethodOptions {
				w.WriteHeader(http.StatusNoContent)
				return
			}
		}
		next.ServeHTTP(w, r)
	})
}

func (h *Holder) allowedOrigin(origin string) bool {
	for _, o := range h.get().CORSOrigins {
		if strings.EqualFold(strings.TrimSuffix(o, "/"), origin) {
			return true
		}
	}
	return false
}

func safeMethod(m string) bool {
	return m == http.MethodGet || m == http.MethodHead || m == http.MethodOptions
}

// sameOrigin : Origin (ou à défaut Sec-Fetch-Site) désigne Symphony lui-même.
// Sans aucun des deux (vieux client, outil en ligne de commande), on s'en
// remet à SameSite=Lax, qui ne joint pas le cookie aux POST d'un autre site.
func sameOrigin(r *http.Request) bool {
	if o := r.Header.Get("Origin"); o != "" {
		u, err := url.Parse(o)
		return err == nil && strings.EqualFold(u.Host, r.Host)
	}
	if s := r.Header.Get("Sec-Fetch-Site"); s != "" {
		return s == "same-origin" || s == "none"
	}
	return true
}
