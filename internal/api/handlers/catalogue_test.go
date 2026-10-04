package handlers

import (
	"maps"
	"testing"

	"github.com/yourorg/symphony/internal/catalogue"
)

func intPtr(n int) *int { return &n }

func TestResolveInputs(t *testing.T) {
	declared := []catalogue.Input{
		{ID: "version", Type: "string", Default: "latest"},
		{ID: "user_id", Type: "string", Required: true},
		{ID: "replicas", Type: "integer", Min: intPtr(1), Max: intPtr(20)},
		{ID: "env", Type: "string", Options: []string{"staging", "production"}},
	}

	tests := []struct {
		name    string
		raw     map[string]any
		want    map[string]string
		wantErr bool
	}{
		{
			name: "défauts appliqués, optionnels absents omis",
			raw:  map[string]any{"user_id": "42"},
			want: map[string]string{"version": "latest", "user_id": "42"},
		},
		{
			name: "entier JSON (float64) et option valide",
			raw:  map[string]any{"user_id": "42", "replicas": float64(3), "env": "staging"},
			want: map[string]string{"version": "latest", "user_id": "42", "replicas": "3", "env": "staging"},
		},
		{
			name: "chaîne vide remplacée par le défaut",
			raw:  map[string]any{"user_id": "42", "version": "  "},
			want: map[string]string{"version": "latest", "user_id": "42"},
		},
		{name: "requis manquant", raw: map[string]any{}, wantErr: true},
		{name: "requis null", raw: map[string]any{"user_id": nil}, wantErr: true},
		{name: "input inconnu refusé", raw: map[string]any{"user_id": "42", "CI_REGISTRY_IMAGE": "evil"}, wantErr: true},
		{name: "entier invalide", raw: map[string]any{"user_id": "42", "replicas": "trois"}, wantErr: true},
		{name: "sous le minimum", raw: map[string]any{"user_id": "42", "replicas": 0}, wantErr: true},
		{name: "au-dessus du maximum", raw: map[string]any{"user_id": "42", "replicas": "21"}, wantErr: true},
		{name: "hors options", raw: map[string]any{"user_id": "42", "env": "dev"}, wantErr: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := resolveInputs(declared, tt.raw)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("erreur attendue, obtenu %v", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("erreur inattendue: %v", err)
			}
			if !maps.Equal(got, tt.want) {
				t.Errorf("obtenu %v, attendu %v", got, tt.want)
			}
		})
	}
}
