package docker

import "testing"

func TestSplitImage(t *testing.T) {
	cases := []struct{ image, tag, repo, wantTag string }{
		{"gitlab.local:5050/root/app", "init", "gitlab.local:5050/root/app", "init"},
		{"gitlab.local:5050/root/app", "", "gitlab.local:5050/root/app", "latest"},
		{"gitlab.local:5050/root/app:v1", "", "gitlab.local:5050/root/app", "v1"},
		{"gitlab.local:5050/root/app:v1", "v2", "gitlab.local:5050/root/app", "v2"},
		{"nginx", "", "nginx", "latest"},
		{"nginx:1.27", "", "nginx", "1.27"},
		{"localhost:5000/app", "latest", "localhost:5000/app", "latest"},
	}
	for _, c := range cases {
		repo, tag := splitImage(c.image, c.tag)
		if repo != c.repo || tag != c.wantTag {
			t.Errorf("splitImage(%q, %q) = %q, %q ; attendu %q, %q", c.image, c.tag, repo, tag, c.repo, c.wantTag)
		}
	}
}

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
