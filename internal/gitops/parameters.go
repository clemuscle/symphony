package gitops

import (
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/santhosh-tekuri/jsonschema/v6"
	"github.com/santhosh-tekuri/jsonschema/v6/kind"
)

// Parameter : un champ du formulaire d'une stack (spec.parameters). Le
// format est volontairement simple, pour un admin qui n'écrit pas de JSON
// Schema : Symphony le traduit en JSON Schema, qui valide les valeurs.
type Parameter struct {
	Name        string `yaml:"name"        json:"name"`
	Title       string `yaml:"title"       json:"title"`
	Description string `yaml:"description" json:"description,omitempty"`
	// Type : string, number, boolean ou choice (une valeur de Choices)
	Type     string   `yaml:"type"     json:"type"`
	Choices  []string `yaml:"choices"  json:"choices,omitempty"`
	Default  any      `yaml:"default"  json:"default,omitempty"`
	Required bool     `yaml:"required" json:"required,omitempty"`
	// When : le paramètre n'existe que si d'autres, déclarés avant lui, ont
	// ces valeurs
	When When `yaml:"when" json:"when,omitempty"`
	// Locked : la valeur est fixée par l'équipe plateforme (Default), montrée
	// mais pas modifiable
	Locked bool `yaml:"locked" json:"locked,omitempty"`
}

// When : paramètre → valeur attendue, ou liste de valeurs acceptées. Toutes
// les conditions doivent être remplies.
type When map[string]any

// Match : les valeurs remplissent-elles la condition ? Un paramètre absent
// (lui-même sans objet) ne la remplit pas.
func (w When) Match(values map[string]any) bool {
	for name, want := range w {
		got, ok := values[name]
		if !ok {
			return false
		}
		if !matchAny(normalize(got), want) {
			return false
		}
	}
	return true
}

func matchAny(got, want any) bool {
	if list, ok := want.([]any); ok {
		for _, w := range list {
			if got == normalize(w) {
				return true
			}
		}
		return false
	}
	return got == normalize(want)
}

// normalize : nombres en float64 (YAML lit des int, JSON des float64)
func normalize(v any) any {
	switch n := v.(type) {
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case uint64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return v
}

var paramNameRe = regexp.MustCompile(`^[a-z][a-z0-9_]*$`)

var paramTypes = map[string]string{"string": "string", "number": "number", "boolean": "boolean", "choice": "string"}

// paramSchema : les paramètres d'une stack et leur JSON Schema compilé
type paramSchema struct {
	params []Parameter
	byName map[string]int
	raw    json.RawMessage
	schema *jsonschema.Schema
}

// compileParameters vérifie les paramètres d'une stack et construit leur
// JSON Schema (draft 2020-12)
func compileParameters(params []Parameter) (*paramSchema, error) {
	ps := &paramSchema{params: params, byName: map[string]int{}}
	properties := map[string]any{}
	var required []string
	var conditions []any
	for i := range params {
		p := &params[i]
		if !paramNameRe.MatchString(p.Name) {
			return nil, fmt.Errorf("name %q invalide (minuscules, chiffres, _ ; commence par une lettre)", p.Name)
		}
		if _, dup := ps.byName[p.Name]; dup {
			return nil, fmt.Errorf("%s déclaré deux fois", p.Name)
		}
		if p.Title == "" {
			p.Title = p.Name
		}
		jsonType, ok := paramTypes[p.Type]
		if !ok {
			return nil, fmt.Errorf("%s : type %q inconnu (string, number, boolean, choice)", p.Name, p.Type)
		}
		if (p.Type == "choice") != (len(p.Choices) > 0) {
			return nil, fmt.Errorf("%s : choices pour le type choice, et lui seul", p.Name)
		}
		prop := map[string]any{"type": jsonType, "title": p.Title}
		if p.Description != "" {
			prop["description"] = p.Description
		}
		if p.Type == "choice" {
			seen := map[string]bool{}
			for _, c := range p.Choices {
				if seen[c] {
					return nil, fmt.Errorf("%s : choix %q en double", p.Name, c)
				}
				seen[c] = true
			}
			prop["enum"] = p.Choices
		}
		if p.Default != nil {
			p.Default = normalize(p.Default)
			if err := checkValue(*p, p.Default); err != nil {
				return nil, fmt.Errorf("%s : default : %w", p.Name, err)
			}
			prop["default"] = p.Default
		}
		if p.Locked {
			if p.Default == nil {
				return nil, fmt.Errorf("%s : locked sans default", p.Name)
			}
			prop["const"] = p.Default
		}
		if p.Required && jsonType == "string" {
			prop["minLength"] = 1 // requis : une chaîne vide ne compte pas
		}
		properties[p.Name] = prop

		if err := ps.checkWhen(p.When, i); err != nil {
			return nil, fmt.Errorf("%s : when : %w", p.Name, err)
		}
		if len(p.When) == 0 {
			if p.Required {
				required = append(required, p.Name)
			}
		} else {
			// Condition remplie : le paramètre existe (et peut être requis) ;
			// sinon il ne doit pas être fourni
			then := map[string]any{}
			if p.Required {
				then["required"] = []string{p.Name}
			}
			conditions = append(conditions, map[string]any{
				"if":   ps.whenSchema(p.When),
				"then": then,
				"else": map[string]any{"properties": map[string]any{p.Name: false}},
			})
		}
		ps.byName[p.Name] = i
	}

	schema := map[string]any{
		"$schema":              "https://json-schema.org/draft/2020-12/schema",
		"type":                 "object",
		"properties":           properties,
		"additionalProperties": false,
	}
	if len(required) > 0 {
		schema["required"] = required
	}
	if len(conditions) > 0 {
		schema["allOf"] = conditions
	}
	raw, err := json.Marshal(schema)
	if err != nil {
		return nil, err
	}
	ps.raw = raw
	doc, err := jsonschema.UnmarshalJSON(strings.NewReader(string(raw)))
	if err != nil {
		return nil, err
	}
	c := jsonschema.NewCompiler()
	if err := c.AddResource("parameters.json", doc); err != nil {
		return nil, err
	}
	if ps.schema, err = c.Compile("parameters.json"); err != nil {
		return nil, fmt.Errorf("JSON Schema : %w", err)
	}
	return ps, nil
}

// checkWhen : une condition ne porte que sur des paramètres déclarés avant
// (les n premiers), avec des valeurs de leur type
func (ps *paramSchema) checkWhen(w When, n int) error {
	for name, want := range w {
		i, ok := ps.byName[name]
		if !ok || i >= n {
			return fmt.Errorf("%s : paramètre inconnu ou déclaré après", name)
		}
		values := []any{want}
		if list, ok := want.([]any); ok {
			values = list
		}
		for _, v := range values {
			if err := checkValue(ps.params[i], normalize(v)); err != nil {
				return fmt.Errorf("%s : %w", name, err)
			}
		}
	}
	return nil
}

// whenSchema : la condition en JSON Schema (les paramètres présents, avec
// une valeur attendue)
func (ps *paramSchema) whenSchema(w When) map[string]any {
	names := make([]string, 0, len(w))
	props := map[string]any{}
	for name, want := range w {
		names = append(names, name)
		if list, ok := want.([]any); ok {
			norm := make([]any, len(list))
			for i, v := range list {
				norm[i] = normalize(v)
			}
			props[name] = map[string]any{"enum": norm}
		} else {
			props[name] = map[string]any{"const": normalize(want)}
		}
	}
	sort.Strings(names)
	return map[string]any{"properties": props, "required": names}
}

// checkValue : v est une valeur possible du paramètre p
func checkValue(p Parameter, v any) error {
	switch p.Type {
	case "string":
		if _, ok := v.(string); !ok {
			return fmt.Errorf("chaîne attendue, %v reçu", v)
		}
	case "choice":
		s, ok := v.(string)
		if !ok || !contains(p.Choices, s) {
			return fmt.Errorf("%v n'est pas l'un des choix (%s)", v, strings.Join(p.Choices, ", "))
		}
	case "number":
		if _, ok := v.(float64); !ok {
			return fmt.Errorf("nombre attendu, %v reçu", v)
		}
	case "boolean":
		if _, ok := v.(bool); !ok {
			return fmt.Errorf("true ou false attendu, %v reçu", v)
		}
	}
	return nil
}

// resolve complète les valeurs (défauts des paramètres qui existent, valeurs
// verrouillées) puis les valide avec le JSON Schema
func (ps *paramSchema) resolve(values map[string]any) (map[string]any, error) {
	out := make(map[string]any, len(ps.params))
	for k, v := range values {
		out[k] = normalize(v)
	}
	for _, p := range ps.params {
		if !p.When.Match(out) {
			continue
		}
		if _, set := out[p.Name]; !set && p.Default != nil {
			out[p.Name] = p.Default
		}
	}
	if err := ps.schema.Validate(out); err != nil {
		var verr *jsonschema.ValidationError
		if errors.As(err, &verr) {
			return nil, ps.explain(verr)
		}
		return nil, err
	}
	return out, nil
}

// explain traduit les erreurs du validateur en une ligne par paramètre
func (ps *paramSchema) explain(verr *jsonschema.ValidationError) error {
	seen := map[string]bool{}
	var msgs []string
	var walk func(e *jsonschema.ValidationError)
	walk = func(e *jsonschema.ValidationError) {
		if len(e.Causes) > 0 {
			for _, c := range e.Causes {
				walk(c)
			}
			return
		}
		param := strings.Join(e.InstanceLocation, ".")
		var msg string
		switch k := e.ErrorKind.(type) {
		case *kind.Required:
			msg = "paramètre " + strings.Join(k.Missing, ", ") + " requis"
		case *kind.MinLength:
			msg = "paramètre " + param + " requis (vide)"
		case *kind.AdditionalProperties:
			msg = "paramètre " + strings.Join(k.Properties, ", ") + " inconnu"
		case *kind.FalseSchema:
			msg = "paramètre " + param + " sans objet ici (sa condition when n'est pas remplie)"
		case *kind.Const:
			msg = fmt.Sprintf("paramètre %s verrouillé à %v", param, k.Want)
		case *kind.Enum:
			msg = fmt.Sprintf("paramètre %s : %v n'est pas l'un des choix", param, k.Got)
		case *kind.Type:
			msg = fmt.Sprintf("paramètre %s : %s attendu", param, typeFR(k.Want))
		default:
			msg = "paramètre " + param + " invalide"
		}
		if !seen[msg] {
			seen[msg] = true
			msgs = append(msgs, msg)
		}
	}
	walk(verr)
	sort.Strings(msgs)
	return errors.New(strings.Join(msgs, " ; "))
}

func typeFR(want []string) string {
	names := map[string]string{"string": "une chaîne", "number": "un nombre", "boolean": "true ou false"}
	out := make([]string, len(want))
	for i, w := range want {
		out[i] = names[w]
		if out[i] == "" {
			out[i] = w
		}
	}
	return strings.Join(out, " ou ")
}

// FormatValue : la valeur d'un paramètre telle qu'elle est écrite dans le
// squelette ({{params.<nom>}})
func FormatValue(v any) string {
	switch x := v.(type) {
	case nil:
		return ""
	case string:
		return x
	case bool:
		return strconv.FormatBool(x)
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1e15 {
			return strconv.FormatInt(int64(x), 10)
		}
		return strconv.FormatFloat(x, 'f', -1, 64)
	}
	return fmt.Sprint(v)
}

// ParamPlaceholders : {{params.<nom>}} → valeur, pour chaque paramètre de la
// stack (vide pour un paramètre sans objet)
func (s Stack) ParamPlaceholders(values map[string]any) []string {
	var pairs []string
	for _, p := range s.Parameters {
		pairs = append(pairs, "{{params."+p.Name+"}}", FormatValue(values[p.Name]))
	}
	return pairs
}
