package web

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"testing/fstest"
)

func TestHandler(t *testing.T) {
	h := handler(fstest.MapFS{
		"index.html":      {Data: []byte("<html>app</html>")},
		"assets/app-1.js": {Data: []byte("js")},
		"favicon.svg":     {Data: []byte("<svg/>")},
	})
	cases := []struct {
		path, body string
		status     int
	}{
		{"/", "<html>app</html>", 200},
		{"/projects/demo", "<html>app</html>", 200}, // route Vue
		{"/assets/app-1.js", "js", 200},
		{"/favicon.svg", "<svg/>", 200},
		{"/assets/absent.js", "", 404},
		{"/../../etc/passwd", "<html>app</html>", 200}, // path.Clean : reste dans dist
	}
	for _, c := range cases {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, c.path, nil))
		if rec.Code != c.status || (c.body != "" && !strings.Contains(rec.Body.String(), c.body)) {
			t.Errorf("%s : %d %q, attendu %d %q", c.path, rec.Code, rec.Body.String(), c.status, c.body)
		}
	}
}

func TestHandlerSansFront(t *testing.T) {
	rec := httptest.NewRecorder()
	handler(fstest.MapFS{".gitkeep": {}}).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != 404 || !strings.Contains(rec.Body.String(), "npm run build") {
		t.Errorf("%d %q", rec.Code, rec.Body.String())
	}
}
