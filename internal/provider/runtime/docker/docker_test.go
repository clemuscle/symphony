package docker

import "testing"

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
