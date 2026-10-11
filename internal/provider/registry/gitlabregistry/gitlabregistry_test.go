package gitlabregistry

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/clemuscle/symphony/internal/provider"
)

func TestListTags(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("PRIVATE-TOKEN") != "tok" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch r.URL.EscapedPath() {
		case "/api/v4/projects/root%2Fmon-api/registry/repositories":
			w.Write([]byte(`[
				{"path":"root/mon-api","tags":[{"name":"a1b2c3"},{"name":"latest"},{"name":"v1"}]},
				{"path":"root/mon-api/cache","tags":[{"name":"build"}]}
			]`))
		case "/api/v4/projects/root%2Fvide/registry/repositories":
			w.Write([]byte(`[]`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()
	p := New("gitlab-registry", srv.URL, "gitlab.local:5050", "tok")

	tags, err := p.ListTags("root/mon-api")
	if err != nil {
		t.Fatal(err)
	}
	if want := []string{"latest", "a1b2c3", "v1"}; !reflect.DeepEqual(tags, want) {
		t.Errorf("tags = %v, attendu %v", tags, want)
	}

	tags, err = p.ListTags("root/vide")
	if err != nil || len(tags) != 0 || tags == nil {
		t.Errorf("projet sans image : tags = %#v, err = %v", tags, err)
	}

	if _, err := p.ListTags("root/absent"); !errors.Is(err, provider.ErrNotFound) {
		t.Errorf("projet absent : err = %v, attendu ErrNotFound", err)
	}

	if got := p.GetImageURL("Root/Mon-API"); got != "gitlab.local:5050/root/mon-api" {
		t.Errorf("GetImageURL = %s", got)
	}
}
