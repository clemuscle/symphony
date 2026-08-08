package api

import (
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"regexp"
	"testing"

	"github.com/yourorg/symphony/internal/auth"
	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/rbac"
)

func newTestServer(groupsEnabled bool) *Server {
	var mgr *rbac.Manager
	if groupsEnabled {
		mgr = rbac.New(rbac.Config{ProjectGroups: []rbac.ProjectGroup{
			{Name: "team-a", OIDCGroups: []string{"a"}},
			{Name: "team-b", OIDCGroups: []string{"b"}},
		}})
	} else {
		mgr = rbac.Default()
	}
	return &Server{rbac: rbac.NewStore(mgr)}
}

func reqWithUser(u *auth.User) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/", nil)
	if u == nil {
		return r
	}
	return r.WithContext(auth.WithUser(context.Background(), u))
}

// TestAuthorizeProject couvre les trois profils (admin / membre / non-membre)
// exigés par la revue architecture-guardian de l'EPIC I, plus les cas limites
// qui ont le plus de chances de régresser silencieusement : scoping désactivé,
// projet legacy sans owner_group, requête sans utilisateur en contexte.
func TestAuthorizeProject(t *testing.T) {
	admin := &auth.User{Role: rbac.RoleAdmin, ProjectGroups: nil}
	memberA := &auth.User{Role: rbac.RoleDeveloper, ProjectGroups: []string{"team-a"}}
	memberB := &auth.User{Role: rbac.RoleDeveloper, ProjectGroups: []string{"team-b"}}

	projectA := &database.Project{Name: "proj-a", OwnerGroup: "team-a"}
	legacyProject := &database.Project{Name: "proj-legacy", OwnerGroup: ""}

	tests := []struct {
		name          string
		groupsEnabled bool
		user          *auth.User
		project       *database.Project
		want          bool
		wantStatus    int
	}{
		{"scoping désactivé — non-membre passe quand même", false, memberB, projectA, true, 0},
		{"admin bypass toujours", true, admin, projectA, true, 0},
		{"membre du bon groupe", true, memberA, projectA, true, 0},
		{"non-membre → 404 (jamais révéler l'existence)", true, memberB, projectA, false, http.StatusNotFound},
		{"projet legacy sans owner_group, non-admin → 404", true, memberA, legacyProject, false, http.StatusNotFound},
		{"projet legacy sans owner_group, admin → ok", true, admin, legacyProject, true, 0},
		{"pas d'utilisateur en contexte → 404", true, nil, projectA, false, http.StatusNotFound},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s := newTestServer(tc.groupsEnabled)
			w := httptest.NewRecorder()
			r := reqWithUser(tc.user)
			got := s.authorizeProject(w, r, tc.project)
			if got != tc.want {
				t.Errorf("authorizeProject() = %v, want %v", got, tc.want)
			}
			if !tc.want && w.Code != tc.wantStatus {
				t.Errorf("status = %d, want %d", w.Code, tc.wantStatus)
			}
			if tc.want && w.Body.Len() != 0 {
				t.Errorf("authorizeProject() ne devrait rien écrire quand il autorise, got body %q", w.Body.String())
			}
		})
	}
}

// TestAuthorizeProjectGuardrail scanne le code source des handlers pour
// vérifier qu'aucun appel à GetProject/GetProjectByRepoPath ne reste sans
// appel à authorizeProject à proximité — c'est la classe de bug qui a produit
// G1 (voir documentation/backlog.md) : un handler qui résout un projet mais
// oublie de vérifier qu'on a le droit d'agir dessus. Un site d'appel peut
// s'exempter explicitement avec le marqueur "authz:no-group-check" sur la
// ligne précédente (ex: transferProjectGroup, déjà admin-only).
//
// Ce n'est pas un test d'intégration (pas de DB) — c'est un filet statique
// bon marché contre l'oubli, pas un remplacement d'une revue de code.
func TestAuthorizeProjectGuardrail(t *testing.T) {
	getProjectCall := regexp.MustCompile(`s\.db\.GetProject(ByRepoPath)?\(`)
	exemptMarker := "authz:no-group-check"
	authzCall := "s.authorizeProject("

	for _, file := range []string{"projects.go", "handlers.go"} {
		data, err := os.ReadFile(file)
		if err != nil {
			t.Fatalf("read %s: %v", file, err)
		}
		lines := splitLines(string(data))
		for i, line := range lines {
			if !getProjectCall.MatchString(line) {
				continue
			}
			exempted := false
			for j := max(0, i-5); j < i; j++ {
				if regexp.MustCompile(exemptMarker).MatchString(lines[j]) {
					exempted = true
					break
				}
			}
			if exempted {
				continue
			}
			window := lines[i:min(i+6, len(lines))]
			found := false
			for _, wl := range window {
				if regexp.MustCompile(regexp.QuoteMeta(authzCall)).MatchString(wl) {
					found = true
					break
				}
			}
			if !found {
				t.Errorf("%s:%d — GetProject sans authorizeProject à proximité (ni marqueur %q) : %s",
					file, i+1, exemptMarker, line)
			}
		}
	}
}

func splitLines(s string) []string {
	var lines []string
	start := 0
	for i, c := range s {
		if c == '\n' {
			lines = append(lines, s[start:i])
			start = i + 1
		}
	}
	lines = append(lines, s[start:])
	return lines
}

