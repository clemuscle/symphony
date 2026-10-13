package docker

import (
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
)

func TestCleanLogs(t *testing.T) {
	frame := func(stream byte, msg string) string {
		n := len(msg)
		return string([]byte{stream, 0, 0, 0, byte(n >> 24), byte(n >> 16), byte(n >> 8), byte(n)}) + msg
	}
	raw := frame(1, "démarrage\n") + frame(2, "panic: boom\n")
	if got, want := cleanLogs(raw), "démarrage\npanic: boom\n"; got != want {
		t.Errorf("cleanLogs = %q, attendu %q", got, want)
	}
	if got := cleanLogs("sortie tty\n"); got != "sortie tty\n" {
		t.Errorf("logs sans en-tête modifiés : %q", got)
	}
}

// fakeDocker : daemon Docker minimal sur une socket unix
func fakeDocker(t *testing.T, handler http.HandlerFunc) *Provider {
	t.Helper()
	sock := filepath.Join(t.TempDir(), "docker.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	srv := &http.Server{Handler: handler}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return New("docker", sock)
}

func TestRemove(t *testing.T) {
	var calls []string
	p := fakeDocker(t, func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.Method+" "+strings.TrimPrefix(r.URL.Path, "/v1.43"))
		switch {
		case r.Method == "GET" && r.URL.Path == "/v1.43/containers/json":
			if got := r.URL.Query().Get("filters"); got != `{"label":["symphony.project=svc"]}` {
				t.Errorf("filtre = %s", got)
			}
			io.WriteString(w, `[{"Id":"aaaaaaaaaaaa1","Image":"reg/svc:1"},{"Id":"bbbbbbbbbbbb2","Image":"reg/svc:1"}]`)
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/v1.43/containers/"):
			if r.URL.Query().Get("force") != "true" {
				t.Error("suppression sans force")
			}
			w.WriteHeader(http.StatusNoContent)
		case r.Method == "GET" && r.URL.Path == "/v1.43/images/json":
			if got := r.URL.Query().Get("filters"); got != `{"reference":["reg/svc"]}` {
				t.Errorf("filtre images = %s", got)
			}
			io.WriteString(w, `[{"RepoTags":["reg/svc:1","reg/svc:latest"]},{"RepoTags":["reg/svc:abc123"]}]`)
		case r.Method == "DELETE" && r.URL.Path == "/v1.43/images/reg/svc:latest":
			w.WriteHeader(http.StatusConflict) // utilisée ailleurs : gardée, pas une erreur
		case r.Method == "DELETE" && strings.HasPrefix(r.URL.Path, "/v1.43/images/"):
			io.WriteString(w, "[]")
		default:
			http.NotFound(w, r)
		}
	})
	if err := p.Remove("svc"); err != nil {
		t.Fatal(err)
	}
	want := []string{
		"GET /containers/json",
		"DELETE /containers/aaaaaaaaaaaa1",
		"DELETE /containers/bbbbbbbbbbbb2",
		"GET /images/json",
		"DELETE /images/reg/svc:1",
		"DELETE /images/reg/svc:latest",
		"DELETE /images/reg/svc:abc123",
	}
	if strings.Join(calls, "\n") != strings.Join(want, "\n") {
		t.Errorf("appels :\n%s\nattendu :\n%s", strings.Join(calls, "\n"), strings.Join(want, "\n"))
	}
}
