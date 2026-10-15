package api

import (
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5/middleware"
)

func TestRequestID(t *testing.T) {
	var seen string
	h := requestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = middleware.GetReqID(r.Context())
	}))

	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest("GET", "/", nil))
	host, _ := os.Hostname()
	if len(seen) != 16 || rec.Header().Get("X-Request-ID") != seen || (host != "" && strings.Contains(seen, host)) {
		t.Errorf("identifiant généré %q (en-tête %q)", seen, rec.Header().Get("X-Request-ID"))
	}

	// Fourni par l'appelant : gardé s'il est sûr, remplacé sinon
	for in, keep := range map[string]bool{"trace-42.a_b": true, "a b<script>": false, strings.Repeat("x", 65): false} {
		req := httptest.NewRequest("GET", "/", nil)
		req.Header.Set("X-Request-ID", in)
		h.ServeHTTP(httptest.NewRecorder(), req)
		if (seen == in) != keep {
			t.Errorf("X-Request-ID %q : obtenu %q", in, seen)
		}
	}
}
