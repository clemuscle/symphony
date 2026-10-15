package api

import (
	"net/http"
	"sort"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
	"gopkg.in/yaml.v3"
)

// TestOpenAPICoversRoutes : chaque route du routeur est décrite dans
// openapi.yaml, et la spec ne décrit pas de route qui n'existe pas
func TestOpenAPICoversRoutes(t *testing.T) {
	var spec struct {
		Paths map[string]map[string]any `yaml:"paths"`
	}
	if err := yaml.Unmarshal(openAPISpec, &spec); err != nil {
		t.Fatal(err)
	}
	documented := map[string]bool{}
	for path, ops := range spec.Paths {
		for method := range ops {
			documented[strings.ToUpper(method)+" "+path] = true
		}
	}

	// Les dépendances ne servent qu'à construire les handlers : aucun n'est appelé
	router := NewServer(nil, nil, nil, nil, nil, nil, tasks.NewRunner(nil, nil), nil, nil).(chi.Routes)
	routed := map[string]bool{}
	err := chi.Walk(router, func(method, route string, _ http.Handler, _ ...func(http.Handler) http.Handler) error {
		if route == "/*" {
			return nil // le front (index.html pour les routes Vue)
		}
		route = strings.Replace(route, "/pipelines/*", "/pipelines/{repo}", 1)
		routed[method+" "+route] = true
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	var missing, extra []string
	for r := range routed {
		if !documented[r] {
			missing = append(missing, r)
		}
	}
	for d := range documented {
		if !routed[d] {
			extra = append(extra, d)
		}
	}
	sort.Strings(missing)
	sort.Strings(extra)
	if len(missing) > 0 {
		t.Errorf("routes absentes de openapi.yaml :\n  %s", strings.Join(missing, "\n  "))
	}
	if len(extra) > 0 {
		t.Errorf("routes décrites mais inexistantes :\n  %s", strings.Join(extra, "\n  "))
	}
}
