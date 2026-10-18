package gitops

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/clemuscle/symphony/internal/catalogue"
)

// Une fiche Backstage telle qu'une équipe l'a déjà, avec d'autres entités
const backstageFiche = `apiVersion: backstage.io/v1alpha1
kind: API
metadata:
  name: payments-api
spec:
  type: openapi
  lifecycle: production
  owner: team-payments
  definition: "openapi: 3.0.0"
---
apiVersion: backstage.io/v1alpha1
kind: Component
metadata:
  name: payments
  description: Paiements par carte
  tags: [go, payments]
  links:
    - url: https://wiki/payments
      title: Runbook
  annotations:
    backstage.io/source-location: url:https://gitlab.local/apps/payments/
    gitlab.com/project-slug: apps/payments
    symphony.io/tier: critical
    symphony.io/language: go
    symphony.io/team-slack: "#payments"
spec:
  type: service
  lifecycle: production
  owner: group:default/team-payments
  system: billing
  providesApis: [payments-api]
  dependsOn: [resource:default/payments-db, component:ledger]
  environments:
    - name: production
      env:
        LOG_LEVEL: info
  slo:
    availability: "99.9%"
`

func TestParseBackstageFiche(t *testing.T) {
	f, err := ParseFiche(backstageFiche, "")
	if err != nil {
		t.Fatal(err)
	}
	if f.Legacy || f.Name != "payments" {
		t.Errorf("fiche : legacy=%v name=%q", f.Legacy, f.Name)
	}
	m, sp := f.Service.Metadata, f.Service.Spec
	if m.Name != "payments" || m.Owner != "team-payments" || m.Tier != "critical" || m.Lifecycle != "production" ||
		m.Description != "Paiements par carte" || m.Team.Slack != "#payments" || len(m.Tags) != 2 {
		t.Errorf("metadata : %+v", m)
	}
	// Les annotations des autres outils sont gardées
	if m.Annotations["gitlab.com/project-slug"] != "apps/payments" {
		t.Errorf("annotations : %v", m.Annotations)
	}
	if sp.Type != "service" || sp.Language != "go" || sp.Repo != "https://gitlab.local/apps/payments" {
		t.Errorf("spec : %+v", sp)
	}
	want := []catalogue.Dependency{{Service: "payments-db", Type: "resource"}, {Service: "ledger", Type: "component"}}
	if !reflect.DeepEqual(sp.Dependencies, want) {
		t.Errorf("dépendances : %+v", sp.Dependencies)
	}
	if len(sp.Links) != 1 || sp.Links[0].Title != "Runbook" || sp.SLO.Availability != "99.9%" ||
		len(sp.Environments) != 1 || sp.Environments[0].Env["LOG_LEVEL"] != "info" {
		t.Errorf("liens, SLO, environnements : %+v", sp)
	}

	// Le catalogue lui donne le nom du projet
	svc, err := ParseService("payments-v2", backstageFiche)
	if err != nil || svc.Metadata.Name != "payments-v2" {
		t.Errorf("ParseService : %v, %q", err, svc.Metadata.Name)
	}
}

func TestParseFicheChoosesComponent(t *testing.T) {
	two := "apiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n  name: front\nspec: {type: website, lifecycle: production, owner: web}\n" +
		"---\napiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n  name: back\nspec: {type: service, lifecycle: production, owner: web}\n"
	f, err := ParseFiche(two, "back")
	if err != nil || f.Service.Spec.Type != "service" {
		t.Errorf("Component back : %v, %+v", err, f.Service.Spec)
	}
	if _, err := ParseFiche(two, "autre"); err == nil || !strings.Contains(err.Error(), "front, back") {
		t.Errorf("plusieurs Component sans le bon nom : %v", err)
	}
	if _, err := ParseFiche(two, ""); err == nil {
		t.Error("découverte d'un fichier à plusieurs Component : erreur attendue")
	}
}

func TestParseFicheErrors(t *testing.T) {
	for name, content := range map[string]string{
		"vide":        "",
		"sans comp.":  "apiVersion: backstage.io/v1alpha1\nkind: API\nmetadata: {name: x}\n",
		"version":     "apiVersion: backstage.io/v1beta1\nkind: Component\nmetadata: {name: x}\n",
		"port":        "apiVersion: backstage.io/v1alpha1\nkind: Component\nmetadata:\n  name: x\n  annotations: {symphony.io/port: http}\n",
		"ancien kind": "kind: GoldenPath\n",
		"ancien+doc":  "kind: Service\n---\nkind: Service\n",
		"yaml":        "kind: [",
	} {
		if _, err := ParseFiche(content, "x"); err == nil {
			t.Errorf("%s : fiche invalide acceptée", name)
		}
	}
}

// L'ancien format est lu, et signalé
func TestParseLegacyFiche(t *testing.T) {
	f, err := ParseFiche("apiVersion: symphony/v1\nkind: Service\nmetadata:\n  name: auth\n  owner: team-identity\n  tier: critical\n  tags: [' ', python]\nspec:\n  language: python\n", "auth")
	if err != nil {
		t.Fatal(err)
	}
	if !f.Legacy || f.Service.Metadata.Owner != "team-identity" || f.Service.Spec.Language != "python" ||
		len(f.Service.Metadata.Tags) != 1 {
		t.Errorf("ancienne fiche : %+v", f)
	}
}

// Ce que Symphony écrit, il le relit à l'identique ; un ancien type de
// dépendance devient une Resource
func TestRenderFicheRoundTrip(t *testing.T) {
	svc := catalogue.Service{
		Metadata: catalogue.Metadata{
			Name: "auth", Description: "Connexion", Owner: "team-identity", Tier: "critical", Lifecycle: "production",
			Team: catalogue.Team{Name: "Identité", Slack: "#identity", Email: "id@x"}, Tags: []string{"python"},
			Labels: map[string]string{"stack": "api-rest-python"},
		},
		Spec: catalogue.ServiceSpec{
			Type: "api", Language: "python", Version: "1.4.0", Repo: "http://gitlab/apps/auth", Docs: "http://docs",
			Links:        []catalogue.Link{{Title: "Repository", URL: "http://gitlab/apps/auth", Icon: "code"}},
			Actions:      []catalogue.Action{{Name: "Relancer", CIProvider: "gitlab-ci"}},
			Dependencies: []catalogue.Dependency{{Service: "postgres-identity", Type: "database"}},
			SLO:          catalogue.SLO{Availability: "99.95%"},
			Infra:        &catalogue.Infra{Kubernetes: "deploy/infra"},
		},
	}
	out, err := RenderFiche(svc, "# en-tête\n")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(out, "# en-tête\napiVersion: backstage.io/v1alpha1\nkind: Component\n") ||
		!strings.Contains(out, "resource:postgres-identity") || !strings.Contains(out, "url:http://gitlab/apps/auth/") {
		t.Errorf("fiche écrite :\n%s", out)
	}
	back, err := ParseService("auth", out)
	if err != nil {
		t.Fatal(err)
	}
	svc.APIVersion, svc.Kind = BackstageAPIVersion, "Component"
	svc.Spec.Dependencies[0].Type = "resource"
	back.Metadata.Annotations = nil // les annotations reprennent les champs
	if !reflect.DeepEqual(back, svc) {
		t.Errorf("relue :\n%+v\nattendue :\n%+v", back, svc)
	}
}

// La fiche de référence est lue sans erreur et renseigne chaque champ
func TestReferenceFiche(t *testing.T) {
	raw, err := os.ReadFile("../../documentation/reference/catalog-info.yaml")
	if err != nil {
		t.Fatal(err)
	}
	f, err := ParseFiche(string(raw), "")
	if err != nil {
		t.Fatalf("référence invalide : %v", err)
	}
	if f.Legacy {
		t.Error("référence à l'ancien format")
	}
	// Champs exclusifs d'un autre : documentés en commentaire
	exclusive := map[string]string{".spec.kubernetes.kustomize": "# kustomize:"}
	var walk func(name string, v reflect.Value)
	walk = func(name string, v reflect.Value) {
		if v.Kind() == reflect.Pointer {
			if v.IsNil() {
				t.Errorf("%s absent de la référence", name)
				return
			}
			if v = v.Elem(); v.Kind() != reflect.Struct {
				return // présent, même à zéro (min: 0)
			}
		}
		switch v.Kind() {
		case reflect.Struct:
			for i := 0; i < v.NumField(); i++ {
				key := strings.Split(v.Type().Field(i).Tag.Get("yaml"), ",")[0]
				walk(name+"."+key, v.Field(i))
			}
		case reflect.Slice:
			// Chaque champ d'un élément est renseigné par au moins un élément
			if v.Len() == 0 || v.Type().Elem().Kind() != reflect.Struct {
				walk(name+"[]", reflect.ValueOf(v.Len()))
				return
			}
			elem := v.Type().Elem()
			for i := 0; i < elem.NumField(); i++ {
				set := -1
				for j := 0; j < v.Len(); j++ {
					if !v.Index(j).Field(i).IsZero() {
						set = j
					}
				}
				key := strings.Split(elem.Field(i).Tag.Get("yaml"), ",")[0]
				if set < 0 {
					t.Errorf("%s[].%s absent de la référence", name, key)
					continue
				}
				walk(name+"[]."+key, v.Index(set).Field(i))
			}
		default:
			if c, ok := exclusive[name]; ok && strings.Contains(string(raw), c) {
				return
			}
			if v.IsZero() {
				t.Errorf("%s absent de la référence", name)
			}
		}
	}
	walk("", reflect.ValueOf(f.Service))
	for _, a := range []string{AnnotationTier, AnnotationLanguage, AnnotationVersion, AnnotationRegistry,
		AnnotationDocs, AnnotationPort, AnnotationTeamName, AnnotationTeamSlack, AnnotationTeamEmail, AnnotationSourceLoc} {
		if f.Service.Metadata.Annotations[a] == "" {
			t.Errorf("annotation %s absente de la référence", a)
		}
	}
}

// Les fiches de la démo sont au format Backstage
func TestDemoFiches(t *testing.T) {
	files, _ := filepath.Glob("../../demo/seed/*.yaml")
	if len(files) == 0 {
		t.Fatal("pas de fiche dans demo/seed")
	}
	for _, file := range files {
		raw, _ := os.ReadFile(file)
		f, err := ParseFiche(string(raw), "")
		if err != nil || f.Legacy || f.Name+".yaml" != filepath.Base(file) {
			t.Errorf("%s : %v (legacy=%v, name=%q)", file, err, f.Legacy, f.Name)
		}
	}
}
