package argocd

import (
	"fmt"
	"path"
	"sort"
	"strings"

	"github.com/clemuscle/symphony/internal/provider"
	"gopkg.in/yaml.v3"
)

// Manifests fournis par la stack du projet (provider.Manifests), copiés dans
// le dossier du service du repo de déploiement : Argo CD ne lit que ce repo.
//
// Chart Helm : copié dans chart/, et values.yaml (à côté) porte ce que
// Symphony décide, selon ce contrat (documentation/reference/stack.yaml) :
// fullnameOverride, replicaCount, image.repository et image.tag,
// containerPort, env, secretEnv, ingress (enabled, host, className),
// podAnnotations.
//
// Base Kustomize : copiée dans base/. Elle décrit un Deployment et un Service
// nommés app, l'image app et un port nommé http ; kustomization.yaml (à côté)
// préfixe les noms par celui du déploiement, fixe l'image, les réplicas et
// les variables (envFrom du premier conteneur), et ajoute l'Ingress.
const (
	helmDir        = "chart"
	helmValuesFile = "values.yaml"
	kustomizeBase  = "base"
	// helmSecretFile : l'ExternalSecret, ajoutée aux templates du chart
	helmSecretFile = "templates/symphony-externalsecret.yaml"
	// baseName : nom des objets de la base Kustomize, préfixés au déploiement
	baseName = "app"
)

// helmValues : values.yaml d'un chart de stack, relu par Stop et Restart
type helmValues struct {
	FullnameOverride string `yaml:"fullnameOverride"`
	ReplicaCount     int    `yaml:"replicaCount"`
	Image            struct {
		Repository string `yaml:"repository"`
		Tag        string `yaml:"tag"`
	} `yaml:"image"`
	ContainerPort int               `yaml:"containerPort"`
	Env           map[string]string `yaml:"env"`
	// SecretEnv : Secret des variables secrètes (créé par External Secrets),
	// vide sans secret
	SecretEnv string `yaml:"secretEnv"`
	Ingress   struct {
		Enabled   bool   `yaml:"enabled"`
		Host      string `yaml:"host"`
		ClassName string `yaml:"className"`
	} `yaml:"ingress"`
	PodAnnotations map[string]string `yaml:"podAnnotations"`
}

const helmValuesHeader = "# Généré par Symphony : image, réplicas, variables et Ingress du service,\n" +
	"# lus par le chart de la stack (chart/)\n"

func (v helmValues) render() (string, error) {
	return encodeYAML(helmValuesHeader, v)
}

func parseHelmValues(content string) (helmValues, error) {
	var v helmValues
	if err := yaml.Unmarshal([]byte(content), &v); err != nil {
		return v, fmt.Errorf("%s: %w", helmValuesFile, err)
	}
	return v, nil
}

func encodeYAML(header string, v any) (string, error) {
	var b strings.Builder
	b.WriteString(header)
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}

// copyInto : les fichiers de la stack sous dir/, sans sortir du dossier
func copyInto(dir string, files map[string]string, out map[string]string) error {
	for rel, content := range files {
		clean := path.Clean(rel)
		if path.IsAbs(clean) || clean == "." || strings.HasPrefix(clean, "../") {
			return fmt.Errorf("manifests de la stack : chemin %q invalide", rel)
		}
		out[path.Join(dir, clean)] = content
	}
	return nil
}

// helmFiles : chart de la stack et values.yaml du déploiement
func (s service) helmFiles(chart map[string]string) (map[string]string, error) {
	if _, ok := chart["Chart.yaml"]; !ok {
		return nil, fmt.Errorf("chart Helm de la stack : Chart.yaml absent")
	}
	files := map[string]string{}
	if err := copyInto(helmDir, chart, files); err != nil {
		return nil, err
	}
	v := helmValues{FullnameOverride: s.Name, ReplicaCount: 1, ContainerPort: s.Port, Env: map[string]string{}}
	v.Image.Repository, v.Image.Tag = s.Image, s.Tag
	for _, e := range s.Env {
		v.Env[e.Name] = e.Value
	}
	if s.IngressHost != "" {
		v.Ingress.Enabled, v.Ingress.Host, v.Ingress.ClassName = true, s.IngressHost, s.IngressClass
	}
	if len(s.Secrets) > 0 {
		v.SecretEnv = s.Name + "-env"
		secret, err := s.renderSecret()
		if err != nil {
			return nil, err
		}
		// Le chart est rendu par Helm : l'ExternalSecret y est une chaîne brute
		if strings.Contains(secret, "`") {
			return nil, fmt.Errorf("secrets : caractère ` interdit dans un nom ou un chemin")
		}
		files[path.Join(helmDir, helmSecretFile)] = "{{`" + secret + "`}}\n"
	}
	values, err := v.render()
	if err != nil {
		return nil, err
	}
	files[helmValuesFile] = values
	return files, nil
}

// kustomizeFiles : base de la stack et kustomization.yaml du déploiement
func (s service) kustomizeFiles(base map[string]string) (map[string]string, error) {
	if _, ok := base[kustomizationFile]; !ok {
		return nil, fmt.Errorf("base Kustomize de la stack : %s absent", kustomizationFile)
	}
	files := map[string]string{}
	if err := copyInto(kustomizeBase, base, files); err != nil {
		return nil, err
	}
	resources := []string{kustomizeBase}
	if s.HasInfra {
		resources = append(resources, infraDir)
	}
	k := kustomization{
		APIVersion: "kustomize.config.k8s.io/v1beta1",
		Kind:       "Kustomization",
		NamePrefix: s.Name + "-",
		Resources:  resources,
		// Deux environnements dans le même namespace : sélecteurs distincts
		Labels: []kustomizeLabels{{
			Pairs:            map[string]string{"app.kubernetes.io/instance": s.Name, "app.kubernetes.io/managed-by": "symphony"},
			IncludeSelectors: true,
		}},
		Images:   []kustomizeImage{{Name: baseName, NewName: s.Image, NewTag: s.Tag}},
		Replicas: []kustomizeCount{{Name: baseName, Count: 1}},
	}
	if s.IngressHost != "" {
		ing := s
		ing.Name = baseName // préfixé par Kustomize, comme le Service qu'il vise
		var b strings.Builder
		if err := ingressTmpl.Execute(&b, ing); err != nil {
			return nil, fmt.Errorf("%s: %w", ingressFile, err)
		}
		files[ingressFile] = b.String()
		k.Resources = append(k.Resources, ingressFile)
	}
	var envFrom []map[string]any
	if len(s.Env) > 0 {
		literals := make([]string, len(s.Env))
		for i, e := range s.Env {
			literals[i] = e.Name + "=" + e.Value
		}
		k.ConfigMapGenerator = []kustomizeConfMap{{Name: baseName + "-env", Literals: literals}}
		envFrom = append(envFrom, map[string]any{"configMapRef": map[string]string{"name": baseName + "-env"}})
	}
	if len(s.Secrets) > 0 {
		secret, err := s.renderSecret()
		if err != nil {
			return nil, err
		}
		files[secretFile] = secret
		k.Resources = append(k.Resources, secretFile)
		envFrom = append(envFrom, map[string]any{"secretRef": map[string]string{"name": s.Name + "-env"}})
	}
	var ops []map[string]any
	if len(envFrom) > 0 {
		ops = append(ops, map[string]any{"op": "add", "path": "/spec/template/spec/containers/0/envFrom", "value": envFrom})
	}
	// Variables de l'infra : elles remplacent l'env du premier conteneur
	if len(s.InfraEnv) > 0 {
		env := make([]map[string]any, len(s.InfraEnv))
		for i, v := range s.InfraEnv {
			env[i] = map[string]any{"name": v.Name, "valueFrom": map[string]any{
				"secretKeyRef": map[string]string{"name": v.Secret, "key": v.Key}}}
		}
		ops = append(ops, map[string]any{"op": "add", "path": "/spec/template/spec/containers/0/env", "value": env})
	}
	if len(ops) > 0 {
		patch, err := yaml.Marshal(ops)
		if err != nil {
			return nil, err
		}
		p := kustomizePatch{Patch: string(patch)}
		p.Target.Kind, p.Target.Name = "Deployment", baseName
		k.Patches = []kustomizePatch{p}
	}
	rendered, err := k.render()
	if err != nil {
		return nil, err
	}
	files[kustomizationFile] = rendered
	return files, nil
}

func (s service) renderSecret() (string, error) {
	var b strings.Builder
	if err := secretTmpl.Execute(&b, s); err != nil {
		return "", fmt.Errorf("%s: %w", secretFile, err)
	}
	return b.String(), nil
}

// stackFiles : les manifests du service selon ce que fournit la stack, et
// son infra
func (s service) stackFiles(m *provider.Manifests, in *provider.Infra) (map[string]string, error) {
	s.HasInfra = in != nil
	var files map[string]string
	var err error
	switch {
	case m == nil:
		files, err = s.files()
	case m.Kind == "helm" && in != nil:
		return nil, fmt.Errorf("infra de la stack : avec un chart Helm, l'infra est dans le chart")
	case m.Kind == "helm":
		return s.helmFiles(m.Files)
	case m.Kind == "kustomize":
		files, err = s.kustomizeFiles(m.Files)
	default:
		return nil, fmt.Errorf("manifests de la stack : type %q inconnu (helm, kustomize)", m.Kind)
	}
	if err != nil || in == nil {
		return files, err
	}
	// Base Kustomize de stack : son préfixe vaut aussi pour l'infra
	infra, err := infraFiles(s.Name, in, m == nil)
	if err != nil {
		return nil, err
	}
	for f, c := range infra {
		files[f] = c
	}
	return files, nil
}

// sortedKeys : clés triées (fichiers à supprimer, dans un ordre stable)
func sortedKeys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
