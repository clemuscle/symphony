package docker

import (
	"io"
	"net"
	"net/http"
	"path/filepath"
	"strings"
	"testing"
	"time"
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

// TestStartAndWatchHealth : la bascule attend l'état healthy du HEALTHCHECK,
// échoue sur unhealthy ou au bout de HealthTimeout, et garde le délai de
// grâce pour une image sans HEALTHCHECK
func TestStartAndWatchHealth(t *testing.T) {
	healthPoll, startupGrace = time.Millisecond, 5*time.Millisecond
	t.Cleanup(func() { healthPoll, startupGrace = time.Second, 3*time.Second })

	cases := []struct {
		name    string
		states  []string // State successifs renvoyés par inspect (le dernier se répète)
		timeout time.Duration
		wantErr string
	}{
		{"healthy", []string{
			`{"Status":"running","Health":{"Status":"starting"}}`,
			`{"Status":"running","Health":{"Status":"starting"}}`,
			`{"Status":"running","Health":{"Status":"healthy"}}`,
		}, time.Second, ""},
		{"unhealthy", []string{
			`{"Status":"running","Health":{"Status":"unhealthy","Log":[{"ExitCode":1,"Output":"connexion refusée"}]}}`,
		}, time.Second, "healthcheck en échec — logs : boom — healthcheck : connexion refusée"},
		{"timeout", []string{
			`{"Status":"running","Health":{"Status":"starting"}}`,
		}, 20 * time.Millisecond, "toujours starting"},
		{"crash", []string{
			`{"Status":"running","Health":{"Status":"starting"}}`,
			`{"Status":"exited","Health":{"Status":"starting"}}`,
		}, time.Second, "ne tient pas au démarrage (état exited)"},
		{"sans healthcheck", []string{`{"Status":"running"}`}, time.Second, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := 0
			p := fakeDocker(t, func(w http.ResponseWriter, r *http.Request) {
				switch {
				case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/start"):
					w.WriteHeader(http.StatusNoContent)
				case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/json"):
					state := c.states[min(n, len(c.states)-1)]
					n++
					io.WriteString(w, `{"Id":"abc","State":`+state+`}`)
				case strings.HasSuffix(r.URL.Path, "/logs"):
					io.WriteString(w, "boom\n")
				default:
					http.NotFound(w, r)
				}
			})
			p.HealthTimeout = c.timeout
			err := p.startAndWatch("abc")
			switch {
			case c.wantErr == "" && err != nil:
				t.Fatalf("erreur inattendue : %v", err)
			case c.wantErr != "" && (err == nil || !strings.Contains(err.Error(), c.wantErr)):
				t.Fatalf("erreur = %v, attendu %q", err, c.wantErr)
			}
		})
	}
}
