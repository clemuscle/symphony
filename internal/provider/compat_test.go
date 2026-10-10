package provider_test

import (
	"testing"

	"github.com/yourorg/symphony/internal/provider"
	githubci "github.com/yourorg/symphony/internal/provider/ci/githubactions"
	gitlabci "github.com/yourorg/symphony/internal/provider/ci/gitlabci"
	githubscm "github.com/yourorg/symphony/internal/provider/scm/github"
	gitlabscm "github.com/yourorg/symphony/internal/provider/scm/gitlab"
)

func TestCheckCompatible(t *testing.T) {
	gitlab := gitlabscm.New("gitlab-local", "http://gitlab.local", "")
	github := githubscm.New("github", "", "")
	glci := gitlabci.New("gitlab-ci", "http://gitlab.local", "")
	ghci := githubci.New("github-actions", "")

	cases := []struct {
		scm  provider.SCMProvider
		ci   provider.CIProvider
		want bool
	}{
		{gitlab, glci, true},
		{github, ghci, true},
		{github, glci, false},
		{gitlab, ghci, false},
	}
	for _, c := range cases {
		err := provider.CheckCompatible(c.scm, c.ci)
		if (err == nil) != c.want {
			t.Errorf("%s + %s : err = %v, compatible attendu = %v", c.scm.ID(), c.ci.ID(), err, c.want)
		}
	}
}
