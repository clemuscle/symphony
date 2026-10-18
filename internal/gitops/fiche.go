package gitops

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strconv"
	"strings"

	"github.com/clemuscle/symphony/internal/catalogue"
	"gopkg.in/yaml.v3"
)

// Fiche d'un projet (catalog-info.yaml) : un Component Backstage
// (backstage.io/v1alpha1), pour qu'une équipe qui a déjà ses fiches les garde.
// Ce que Backstage n'a pas passe par des annotations symphony.io/… (valeurs
// simples) et par des champs de spec en plus (environnements, actions, SLO,
// manifests et infra), que Backstage accepte. L'ancien format (symphony/v1,
// kind Service) est lu jusqu'à la v1.0 et signalé dans Administration.

const (
	BackstageAPIVersion = "backstage.io/v1alpha1"
	LegacyAPIVersion    = "symphony/v1"

	// Annotations de la fiche lues par Symphony
	AnnotationTier       = "symphony.io/tier"
	AnnotationLanguage   = "symphony.io/language"
	AnnotationVersion    = "symphony.io/version"
	AnnotationRegistry   = "symphony.io/registry"
	AnnotationDocs       = "symphony.io/docs"
	AnnotationPort       = "symphony.io/port" // port de l'appli d'un projet découvert
	AnnotationTeamName   = "symphony.io/team-name"
	AnnotationTeamSlack  = "symphony.io/team-slack"
	AnnotationTeamEmail  = "symphony.io/team-email"
	AnnotationSourceLoc  = "backstage.io/source-location"
	sourceLocationPrefix = "url:"
)

// Fiche : une fiche lue, avec ce que la page Administration et la découverte
// en retiennent
type Fiche struct {
	Service catalogue.Service
	// Name : metadata.name tel qu'écrit dans la fiche (le catalogue lui
	// préfère le nom du projet)
	Name string
	// Legacy : ancien format symphony/v1
	Legacy bool
}

// backstageEntity : un Component, ou une autre entité d'un fichier qui en
// contient plusieurs (API, Resource… ignorées)
type backstageEntity struct {
	APIVersion string            `yaml:"apiVersion"`
	Kind       string            `yaml:"kind"`
	Metadata   backstageMetadata `yaml:"metadata"`
	Spec       backstageSpec     `yaml:"spec"`
}

type backstageMetadata struct {
	Name        string            `yaml:"name"`
	Description string            `yaml:"description,omitempty"`
	Labels      map[string]string `yaml:"labels,omitempty"`
	Annotations map[string]string `yaml:"annotations,omitempty"`
	Tags        []string          `yaml:"tags,omitempty"`
	Links       []backstageLink   `yaml:"links,omitempty"`
}

type backstageLink struct {
	URL   string `yaml:"url"`
	Title string `yaml:"title,omitempty"`
	Icon  string `yaml:"icon,omitempty"`
}

type backstageSpec struct {
	Type      string   `yaml:"type"`
	Lifecycle string   `yaml:"lifecycle"`
	Owner     string   `yaml:"owner"`
	System    string   `yaml:"system,omitempty"`
	DependsOn []string `yaml:"dependsOn,omitempty"`

	// Champs de Symphony
	Environments []catalogue.Environment `yaml:"environments,omitempty"`
	Actions      []catalogue.Action      `yaml:"actions,omitempty"`
	SLO          *catalogue.SLO          `yaml:"slo,omitempty"`
	Kubernetes   *catalogue.Kubernetes   `yaml:"kubernetes,omitempty"`
	Infra        *catalogue.Infra        `yaml:"infra,omitempty"`
}

// ParseService lit la fiche d'un projet et lui donne le nom du projet. Une
// fiche invalide est une erreur, pas une fiche vide.
func ParseService(name, content string) (catalogue.Service, error) {
	f, err := ParseFiche(content, name)
	return f.Service, err
}

// ParseFiche lit une fiche. Un fichier Backstage peut décrire plusieurs
// entités : le Component retenu est celui qui s'appelle name, ou le seul du
// fichier. name vide : le nom de la fiche est gardé (découverte).
func ParseFiche(content, name string) (Fiche, error) {
	docs, err := splitDocuments(content)
	if err != nil {
		return Fiche{}, err
	}
	var components []backstageEntity
	for _, doc := range docs {
		var head struct {
			APIVersion string `yaml:"apiVersion"`
			Kind       string `yaml:"kind"`
		}
		if err := doc.Decode(&head); err != nil {
			return Fiche{}, err
		}
		if !strings.HasPrefix(head.APIVersion, "backstage.io/") && head.Kind != "Component" {
			if len(docs) > 1 {
				return Fiche{}, errors.New("plusieurs documents : seul le format Backstage (backstage.io/v1alpha1) en accepte plusieurs")
			}
			return parseLegacy(doc, name)
		}
		if head.Kind != "Component" {
			continue // API, Resource, System… : pas un service
		}
		var e backstageEntity
		if err := doc.Decode(&e); err != nil {
			return Fiche{}, err
		}
		components = append(components, e)
	}
	var picked *backstageEntity
	switch {
	case len(components) == 0:
		return Fiche{}, errors.New("aucune entité kind: Component dans la fiche")
	case len(components) == 1:
		picked = &components[0]
	default:
		names := make([]string, len(components))
		for i := range components {
			names[i] = components[i].Metadata.Name
			if components[i].Metadata.Name == name {
				picked = &components[i]
			}
		}
		if picked == nil {
			return Fiche{}, fmt.Errorf("plusieurs Component (%s) : l'un doit s'appeler %q", strings.Join(names, ", "), name)
		}
	}
	return fromBackstage(*picked, name)
}

// splitDocuments : les documents YAML du fichier (séparés par ---), sans les
// documents vides
func splitDocuments(content string) ([]*yaml.Node, error) {
	dec := yaml.NewDecoder(strings.NewReader(content))
	var docs []*yaml.Node
	for {
		var n yaml.Node
		err := dec.Decode(&n)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, err
		}
		if len(n.Content) == 0 || n.Content[0].Kind == yaml.ScalarNode && n.Content[0].Tag == "!!null" {
			continue
		}
		docs = append(docs, &n)
	}
	if len(docs) == 0 {
		return nil, errors.New("fiche vide")
	}
	return docs, nil
}

func parseLegacy(doc *yaml.Node, name string) (Fiche, error) {
	var svc catalogue.Service
	if err := doc.Decode(&svc); err != nil {
		return Fiche{}, err
	}
	if svc.Kind != "" && svc.Kind != "Service" {
		return Fiche{}, errors.New("kind " + svc.Kind + " : Component (backstage.io/v1alpha1) attendu")
	}
	f := Fiche{Name: svc.Metadata.Name, Legacy: true}
	svc.APIVersion, svc.Kind = LegacyAPIVersion, "Service"
	f.Service = finish(svc, name)
	return f, nil
}

func fromBackstage(e backstageEntity, name string) (Fiche, error) {
	if e.APIVersion != BackstageAPIVersion {
		return Fiche{}, fmt.Errorf("apiVersion %s : %s attendu", e.APIVersion, BackstageAPIVersion)
	}
	a := e.Metadata.Annotations
	svc := catalogue.Service{
		APIVersion: e.APIVersion,
		Kind:       e.Kind,
		Metadata: catalogue.Metadata{
			Name:        e.Metadata.Name,
			Description: e.Metadata.Description,
			Owner:       entityName(e.Spec.Owner),
			Team: catalogue.Team{
				Name:  a[AnnotationTeamName],
				Slack: a[AnnotationTeamSlack],
				Email: a[AnnotationTeamEmail],
			},
			Tags:        e.Metadata.Tags,
			Tier:        a[AnnotationTier],
			Lifecycle:   e.Spec.Lifecycle,
			Labels:      e.Metadata.Labels,
			Annotations: a,
		},
		Spec: catalogue.ServiceSpec{
			Type:         e.Spec.Type,
			Language:     a[AnnotationLanguage],
			Version:      a[AnnotationVersion],
			Repo:         strings.TrimSuffix(strings.TrimPrefix(a[AnnotationSourceLoc], sourceLocationPrefix), "/"),
			Registry:     a[AnnotationRegistry],
			Docs:         a[AnnotationDocs],
			Actions:      e.Spec.Actions,
			Environments: e.Spec.Environments,
			Kubernetes:   e.Spec.Kubernetes,
			Infra:        e.Spec.Infra,
		},
	}
	if p := a[AnnotationPort]; p != "" {
		if n, err := strconv.Atoi(p); err != nil || n < 1 || n > 65535 {
			return Fiche{}, fmt.Errorf("annotation %s : port invalide %q", AnnotationPort, p)
		}
	}
	for _, l := range e.Metadata.Links {
		svc.Spec.Links = append(svc.Spec.Links, catalogue.Link{Title: l.Title, URL: l.URL, Icon: l.Icon})
	}
	for _, ref := range e.Spec.DependsOn {
		kind, n := entityKind(ref)
		svc.Spec.Dependencies = append(svc.Spec.Dependencies, catalogue.Dependency{Service: n, Type: kind})
	}
	if e.Spec.SLO != nil {
		svc.Spec.SLO = *e.Spec.SLO
	}
	return Fiche{Service: finish(svc, name), Name: e.Metadata.Name}, nil
}

// finish : le nom du projet (s'il est connu) et les étiquettes nettoyées
func finish(svc catalogue.Service, name string) catalogue.Service {
	if name != "" {
		if svc.Metadata.Name != "" && svc.Metadata.Name != name {
			slog.Debug("catalogue : metadata.name ignoré, nom du projet utilisé",
				"fiche", svc.Metadata.Name, "project", name)
		}
		svc.Metadata.Name = name
	}
	svc.Metadata.Tags = cleanTags(svc.Metadata.Tags)
	return svc
}

// entityName : le nom d'une référence d'entité Backstage
// ([<kind>:][<namespace>/]<nom>)
func entityName(ref string) string {
	_, name := entityKind(ref)
	return name
}

// entityKind : le kind (minuscules, vide s'il est absent) et le nom d'une
// référence d'entité
func entityKind(ref string) (string, string) {
	kind, rest, ok := strings.Cut(strings.TrimSpace(ref), ":")
	if !ok {
		kind, rest = "", kind
	}
	if _, name, ok := strings.Cut(rest, "/"); ok {
		rest = name
	}
	return strings.ToLower(kind), rest
}

// dependencyKinds : kinds Backstage d'une dépendance ; les types de l'ancien
// format (database, cache…) deviennent des Resource
var dependencyKinds = []string{"component", "resource", "api", "system"}

// RenderFiche écrit la fiche d'un projet au format Backstage, précédée de
// header (commentaires)
func RenderFiche(svc catalogue.Service, header string) (string, error) {
	m, sp := svc.Metadata, svc.Spec
	annotations := map[string]string{}
	for k, v := range m.Annotations {
		annotations[k] = v
	}
	for k, v := range map[string]string{
		AnnotationTier:      m.Tier,
		AnnotationLanguage:  sp.Language,
		AnnotationVersion:   sp.Version,
		AnnotationRegistry:  sp.Registry,
		AnnotationDocs:      sp.Docs,
		AnnotationTeamName:  m.Team.Name,
		AnnotationTeamSlack: m.Team.Slack,
		AnnotationTeamEmail: m.Team.Email,
	} {
		if v != "" {
			annotations[k] = v
		}
	}
	if sp.Repo != "" {
		annotations[AnnotationSourceLoc] = sourceLocationPrefix + strings.TrimSuffix(sp.Repo, "/") + "/"
	}
	e := backstageEntity{
		APIVersion: BackstageAPIVersion,
		Kind:       "Component",
		Metadata: backstageMetadata{
			Name:        m.Name,
			Description: m.Description,
			Labels:      m.Labels,
			Tags:        m.Tags,
		},
		Spec: backstageSpec{
			Type:         firstNonEmpty(sp.Type, "service"),
			Lifecycle:    firstNonEmpty(m.Lifecycle, "experimental"),
			Owner:        firstNonEmpty(m.Owner, "unknown"),
			Environments: sp.Environments,
			Actions:      sp.Actions,
			Kubernetes:   sp.Kubernetes,
			Infra:        sp.Infra,
		},
	}
	if len(annotations) > 0 {
		e.Metadata.Annotations = annotations
	}
	for _, l := range sp.Links {
		e.Metadata.Links = append(e.Metadata.Links, backstageLink{URL: l.URL, Title: l.Title, Icon: l.Icon})
	}
	for _, d := range sp.Dependencies {
		kind := strings.ToLower(d.Type)
		if !slices.Contains(dependencyKinds, kind) {
			kind = "resource"
		}
		e.Spec.DependsOn = append(e.Spec.DependsOn, kind+":"+d.Service)
	}
	if sp.SLO != (catalogue.SLO{}) {
		slo := sp.SLO
		e.Spec.SLO = &slo
	}

	var buf bytes.Buffer
	buf.WriteString(header)
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(e); err != nil {
		return "", err
	}
	return buf.String(), nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

// cleanTags retire les étiquettes vides
func cleanTags(tags []string) []string {
	out := tags[:0]
	for _, t := range tags {
		if t = strings.TrimSpace(t); t != "" {
			out = append(out, t)
		}
	}
	return out
}
