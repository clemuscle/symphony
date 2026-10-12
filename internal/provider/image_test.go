package provider

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
		repo, tag := SplitImage(c.image, c.tag)
		if repo != c.repo || tag != c.wantTag {
			t.Errorf("SplitImage(%q, %q) = %q, %q ; attendu %q, %q", c.image, c.tag, repo, tag, c.repo, c.wantTag)
		}
	}
}
