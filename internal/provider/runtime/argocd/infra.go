package argocd

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"path"
	"slices"
	"sort"
	"strings"

	"github.com/clemuscle/symphony/internal/provider"
	"gopkg.in/yaml.v3"
)

// Infra d'un service (provider.Infra) : les manifests d'opérateurs de sa
// stack (un Cluster CloudNativePG…), copiés dans infra/src/ du dossier du
// service et appliqués par la même Application, dans chaque environnement.
// infra/kustomization.yaml (écrit par Symphony) les nomme
// <déploiement>-<nom> et les protège de l'élagage : un objet retiré de Git
// reste dans le cluster (une base n'est jamais supprimée par erreur), seule la
// suppression du projet (Application supprimée en cascade) l'emporte.
const (
	infraDir        = "infra"
	infraSrc        = "src"
	infraAnnotation = "symphony/infra" // sur l'Application : types des objets de l'infra
	// pruneOff : Argo CD ne supprime pas l'objet à la synchronisation
	pruneOff = "argocd.argoproj.io/sync-options"
)

// infraVar : variable de l'appli lue dans un secret créé par l'infra
type infraVar struct{ Name, Secret, Key string }

// infraEnv : les variables de l'infra, secrets nommés pour le déploiement
func infraEnv(name string, in *provider.Infra) []infraVar {
	if in == nil {
		return nil
	}
	vars := make([]infraVar, 0, len(in.Env))
	for v, ref := range in.Env {
		vars = append(vars, infraVar{Name: v, Secret: name + "-" + ref.Secret, Key: ref.Key})
	}
	sort.Slice(vars, func(i, j int) bool { return vars[i].Name < vars[j].Name })
	return vars
}

// infraFiles : les fichiers de l'infra sous infra/, et son kustomization.yaml.
// prefix : vrai si les noms doivent y être préfixés (manifests générés ; une
// base Kustomize de stack préfixe déjà tout).
func infraFiles(name string, in *provider.Infra, prefix bool) (map[string]string, error) {
	if len(in.Files) == 0 {
		return nil, fmt.Errorf("infra de la stack : dossier vide")
	}
	files := map[string]string{}
	if err := copyInto(path.Join(infraDir, infraSrc), in.Files, files); err != nil {
		return nil, err
	}
	// Une infra qui a son kustomization.yaml est une base ; sinon, chaque
	// fichier YAML est une ressource
	resources := []string{infraSrc}
	if _, ok := in.Files[kustomizationFile]; !ok {
		resources = nil
		for rel := range in.Files {
			if ext := path.Ext(rel); ext == ".yaml" || ext == ".yml" {
				resources = append(resources, path.Join(infraSrc, path.Clean(rel)))
			}
		}
		sort.Strings(resources)
	}
	if len(resources) == 0 {
		return nil, fmt.Errorf("infra de la stack : aucun manifest YAML")
	}
	k := kustomization{
		APIVersion:        "kustomize.config.k8s.io/v1beta1",
		Kind:              "Kustomization",
		Resources:         resources,
		CommonAnnotations: map[string]string{pruneOff: "Prune=false"},
		Labels: []kustomizeLabels{{
			Pairs: map[string]string{"app.kubernetes.io/instance": name, "app.kubernetes.io/managed-by": "symphony",
				"app.kubernetes.io/component": "infra"},
		}},
	}
	if prefix {
		k.NamePrefix = name + "-"
	}
	rendered, err := encodeYAML("# Généré par Symphony : l'infra du service (src/), nommée pour ce déploiement\n", k)
	if err != nil {
		return nil, err
	}
	files[path.Join(infraDir, kustomizationFile)] = rendered
	return files, nil
}

// infraKinds : les types des objets de l'infra (hors Secret, ConfigMap et
// Service), relevés dans ses manifests ; la page du projet montre leur état
func infraKinds(in *provider.Infra) string {
	if in == nil {
		return ""
	}
	seen := map[string]bool{}
	for rel, content := range in.Files {
		if ext := path.Ext(rel); ext != ".yaml" && ext != ".yml" || path.Base(rel) == kustomizationFile {
			continue
		}
		dec := yaml.NewDecoder(strings.NewReader(content))
		for {
			var doc struct {
				Kind string `yaml:"kind"`
			}
			if err := dec.Decode(&doc); err != nil {
				break
			}
			switch doc.Kind {
			case "", "Secret", "ConfigMap", "Service":
			default:
				seen[doc.Kind] = true
			}
		}
	}
	return strings.Join(sortedKeys(seen), ",")
}

// infraStatus : les objets de l'Application dont le type est celui de
// l'infra, et leur santé. Argo CD 3 ne la garde plus dans le statut de
// l'Application : elle est lue dans son arbre de ressources (un appel de plus,
// pour les seules Applications qui ont une infra).
func (p *Provider) infraStatus(app application) []provider.InfraResource {
	kinds := strings.Split(app.Metadata.Annotations[infraAnnotation], ",")
	if len(kinds) == 0 || kinds[0] == "" {
		return nil
	}
	resources := []appResource{}
	data, status, err := p.argo("GET", "/api/v1/applications/"+url.PathEscape(app.Metadata.Name)+"/resource-tree", nil)
	if err == nil && status == http.StatusOK {
		var tree struct {
			Nodes []appResource `json:"nodes"`
		}
		if json.Unmarshal(data, &tree) == nil {
			resources = tree.Nodes
		}
	} else if app.Status != nil {
		resources = app.Status.Resources // santé absente : Unknown
	}
	var out []provider.InfraResource
	for _, r := range resources {
		if !slices.Contains(kinds, r.Kind) {
			continue
		}
		health := r.Health.Status
		if health == "" {
			health = "Unknown"
		}
		out = append(out, provider.InfraResource{Kind: r.Kind, Name: r.Name, Health: health})
	}
	return out
}
