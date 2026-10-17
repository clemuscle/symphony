package gitops

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

const paramsYAML = `
- name: version
  type: choice
  choices: ["1.22", "1.23"]
  default: "1.22"
  locked: true
- name: replicas
  title: Réplicas
  type: number
  default: 2
- name: metrics
  type: boolean
  default: true
- name: metrics_path
  type: string
  default: /metrics
  required: true
  when: { metrics: true }
- name: database
  type: choice
  choices: [aucune, postgres, mysql]
  default: aucune
- name: db_name
  type: string
  required: true
  when: { database: [postgres, mysql] }
- name: db_pool
  type: number
  when: { db_name: base }
`

func testParams(t *testing.T) *paramSchema {
	t.Helper()
	var params []Parameter
	if err := yaml.Unmarshal([]byte(paramsYAML), &params); err != nil {
		t.Fatal(err)
	}
	ps, err := compileParameters(params)
	if err != nil {
		t.Fatal(err)
	}
	return ps
}

func TestResolveParameters(t *testing.T) {
	ps := testParams(t)
	// Défauts complétés ; un paramètre sans objet reste absent
	got, err := ps.resolve(nil)
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]any{"version": "1.22", "replicas": float64(2), "metrics": true, "metrics_path": "/metrics", "database": "aucune"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("resolve(nil) = %v\nattendu %v", got, want)
	}

	// Valeurs JSON (nombres en float64) et conditions en cascade
	var in map[string]any
	json.Unmarshal([]byte(`{"replicas": 3, "metrics": false, "database": "postgres", "db_name": "base", "db_pool": 10}`), &in)
	got, err = ps.resolve(in)
	if err != nil {
		t.Fatal(err)
	}
	if got["db_pool"] != float64(10) || got["replicas"] != float64(3) || got["version"] != "1.22" {
		t.Errorf("resolve = %v", got)
	}
	if _, ok := got["metrics_path"]; ok {
		t.Error("metrics_path sans objet (metrics: false) ne doit pas être complété")
	}
}

func TestResolveParametersInvalid(t *testing.T) {
	ps := testParams(t)
	cases := map[string]struct {
		values map[string]any
		want   string
	}{
		"verrouillé":       {map[string]any{"version": "1.23"}, "version verrouillé à 1.22"},
		"hors des choix":   {map[string]any{"database": "oracle"}, "database : oracle n'est pas l'un des choix"},
		"type":             {map[string]any{"replicas": "trois"}, "replicas : un nombre attendu"},
		"inconnu":          {map[string]any{"couleur": "bleu"}, "paramètre couleur inconnu"},
		"requis":           {map[string]any{"database": "postgres"}, "paramètre db_name requis"},
		"requis vide":      {map[string]any{"metrics_path": ""}, "metrics_path requis (vide)"},
		"sans objet":       {map[string]any{"metrics": false, "metrics_path": "/m"}, "metrics_path sans objet"},
		"sans objet chaîn": {map[string]any{"db_pool": 3}, "db_pool sans objet"},
	}
	for name, c := range cases {
		_, err := ps.resolve(c.values)
		if err == nil || !strings.Contains(err.Error(), c.want) {
			t.Errorf("%s : erreur %v, attendu « %s »", name, err, c.want)
		}
	}
}

func TestCompileParametersInvalid(t *testing.T) {
	cases := map[string]string{
		"nom":                 `[{name: Version, type: string}]`,
		"doublon":             `[{name: a, type: string}, {name: a, type: string}]`,
		"type":                `[{name: a, type: date}]`,
		"choice sans choix":   `[{name: a, type: choice}]`,
		"choix hors choice":   `[{name: a, type: string, choices: [x]}]`,
		"choix en double":     `[{name: a, type: choice, choices: [x, x]}]`,
		"défaut hors choix":   `[{name: a, type: choice, choices: [x], default: y}]`,
		"défaut mal typé":     `[{name: a, type: number, default: deux}]`,
		"verrouillé sans déf": `[{name: a, type: string, locked: true}]`,
		"when inconnu":        `[{name: a, type: string, when: {b: x}}]`,
		"when déclaré après":  `[{name: a, type: string, when: {b: x}}, {name: b, type: string}]`,
		"when sur soi":        `[{name: a, type: boolean, when: {a: true}}]`,
		"when mal typé":       `[{name: a, type: boolean}, {name: b, type: string, when: {a: oui}}]`,
		"when hors choix":     `[{name: a, type: choice, choices: [x]}, {name: b, type: string, when: {a: [x, y]}}]`,
	}
	for name, src := range cases {
		var params []Parameter
		if err := yaml.Unmarshal([]byte(src), &params); err != nil {
			t.Fatalf("%s : %v", name, err)
		}
		if _, err := compileParameters(params); err == nil {
			t.Errorf("%s : erreur attendue", name)
		}
	}
}

// Le JSON Schema servi par l'API décrit le formulaire
func TestParametersSchema(t *testing.T) {
	ps := testParams(t)
	var schema struct {
		Properties map[string]map[string]any `json:"properties"`
		AllOf      []map[string]any          `json:"allOf"`
		Additional bool                      `json:"additionalProperties"`
	}
	if err := json.Unmarshal(ps.raw, &schema); err != nil {
		t.Fatal(err)
	}
	if schema.Additional || len(schema.Properties) != 7 || len(schema.AllOf) != 3 ||
		schema.Properties["version"]["const"] != "1.22" || schema.Properties["replicas"]["title"] != "Réplicas" {
		t.Errorf("schéma = %s", ps.raw)
	}
}

func TestFormatValue(t *testing.T) {
	cases := map[string]any{"": nil, "x": "x", "true": true, "3": float64(3), "2.5": 2.5}
	for want, v := range cases {
		if got := FormatValue(v); got != want {
			t.Errorf("FormatValue(%v) = %q, attendu %q", v, got, want)
		}
	}
}

func TestKeep(t *testing.T) {
	s := Stack{Files: []FileRule{
		{Path: "cmd/metrics.go", When: When{"metrics": true}},
		{Path: "deploy/chart/", When: When{"helm": true}},
	}}
	values := map[string]any{"metrics": false, "helm": true}
	for file, want := range map[string]bool{
		"cmd/metrics.go": false, "cmd/main.go": true, "deploy/chart/Chart.yaml": true, "deploy/chartx": true,
	} {
		if got := s.Keep(file, values); got != want {
			t.Errorf("Keep(%s) = %v", file, got)
		}
	}
	if s.Keep("deploy/chart/values.yaml", map[string]any{}) {
		t.Error("paramètre absent : condition non remplie")
	}
}
