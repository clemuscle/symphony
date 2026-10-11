// Package auth authentifie les utilisateurs (OIDC, modèle Backend-for-Frontend)
// et applique les rôles : viewer < developer < admin.
//
// Le navigateur ne voit jamais de jeton : Symphony fait le flux Authorization
// Code + PKCE avec le fournisseur d'identité et pose un cookie de session signé
// (HttpOnly, SameSite=Lax). Les scripts et la CI peuvent présenter un jeton
// d'accès en Authorization: Bearer.
package auth

import (
	"context"
	"fmt"
	"sort"
)

type Role string

const (
	RoleNone      Role = ""
	RoleViewer    Role = "viewer"    // lecture seule
	RoleDeveloper Role = "developer" // crée des projets, déploie, lance pipelines et actions
	RoleAdmin     Role = "admin"     // supprime des projets, synchronise, lit l'audit
)

var rank = map[Role]int{RoleNone: 0, RoleViewer: 1, RoleDeveloper: 2, RoleAdmin: 3}

// AtLeast indique si r a au moins les droits de min
func (r Role) AtLeast(min Role) bool { return rank[r] >= rank[min] }

// RoleMapper traduit des groupes OIDC en rôle Symphony. L'indirection
// (configuration, pas de groupe codé en dur) permet de suivre l'annuaire de
// l'entreprise, y compris quand il émet des identifiants illisibles.
type RoleMapper struct {
	groupRole map[string]Role
}

// NewRoleMapper prend la section roles de la configuration : rôle → groupes
func NewRoleMapper(roles map[string][]string) (*RoleMapper, error) {
	m := &RoleMapper{groupRole: map[string]Role{}}
	for name, groups := range roles {
		role := Role(name)
		if role == RoleNone || rank[role] == 0 {
			return nil, fmt.Errorf("rôle %q inconnu (viewer, developer, admin)", name)
		}
		for _, g := range groups {
			if rank[role] > rank[m.groupRole[g]] {
				m.groupRole[g] = role
			}
		}
	}
	return m, nil
}

// Resolve renvoie le rôle le plus élevé parmi les groupes, ou RoleNone
func (m *RoleMapper) Resolve(groups []string) Role {
	best := RoleNone
	for _, g := range groups {
		if r := m.groupRole[g]; rank[r] > rank[best] {
			best = r
		}
	}
	return best
}

// User est l'utilisateur de la requête
type User struct {
	Subject string   `json:"subject"`
	Name    string   `json:"name"`
	Email   string   `json:"email"`
	Groups  []string `json:"groups"`
	Role    Role     `json:"role"`
}

// ID identifie l'utilisateur dans l'audit : e-mail, sinon nom, sinon sujet
func (u *User) ID() string {
	switch {
	case u.Email != "":
		return u.Email
	case u.Name != "":
		return u.Name
	}
	return u.Subject
}

// anonymous est l'utilisateur quand l'authentification est désactivée
var anonymous = &User{Subject: "anonyme", Name: "anonyme", Role: RoleAdmin}

type ctxKey struct{}

func withUser(ctx context.Context, u *User) context.Context {
	return context.WithValue(ctx, ctxKey{}, u)
}

// UserFrom renvoie l'utilisateur de la requête (nil hors des routes protégées)
func UserFrom(ctx context.Context) *User {
	u, _ := ctx.Value(ctxKey{}).(*User)
	return u
}

func sortedGroups(groups []string) []string {
	g := append([]string(nil), groups...)
	sort.Strings(g)
	return g
}
