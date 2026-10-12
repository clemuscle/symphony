package argocd

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"text/template"

	"gopkg.in/yaml.v3"
)

// Fichiers d'un service dans le repo de déploiement (<path>/<nom>/). Argo CD
// les lit comme une application Kustomize : kustomization.yaml porte ce qui
// change d'un déploiement à l'autre (tag de l'image, réplicas, redémarrage),
// les autres fichiers décrivent le service.
const (
	kustomizationFile = "kustomization.yaml"
	deploymentFile    = "deployment.yaml"
	serviceFile       = "service.yaml"
	ingressFile       = "ingress.yaml"
)

// restartAnnotation : changer sa valeur relance les pods (nouveau ReplicaSet)
const restartAnnotation = "symphony/restarted-at"

// nameRe : nom DNS (RFC 1123), exigé par Kubernetes pour les objets et par
// Argo CD pour les Applications
var nameRe = regexp.MustCompile(`^[a-z0-9]([-a-z0-9]*[a-z0-9])?$`)

func validName(name string) error {
	if len(name) > 63 || !nameRe.MatchString(name) {
		return fmt.Errorf("nom %q invalide pour Kubernetes (minuscules, chiffres, tirets, 63 caractères max.)", name)
	}
	return nil
}

// service : ce que Symphony déploie, rendu en manifests
type service struct {
	Name         string
	Image        string // sans tag : le tag est dans kustomization.yaml
	Tag          string
	Port         int
	Env          []envVar
	IngressHost  string // vide : pas d'Ingress
	IngressClass string // vide : classe par défaut du cluster
}

type envVar struct{ Name, Value string }

func sortedEnv(vars map[string]string) []envVar {
	env := make([]envVar, 0, len(vars))
	for k, v := range vars {
		env = append(env, envVar{k, v})
	}
	sort.Slice(env, func(i, j int) bool { return env[i].Name < env[j].Name })
	return env
}

// kustomization : format de kustomization.yaml, relu par Stop et Restart
type kustomization struct {
	APIVersion        string            `yaml:"apiVersion"`
	Kind              string            `yaml:"kind"`
	Resources         []string          `yaml:"resources"`
	CommonAnnotations map[string]string `yaml:"commonAnnotations,omitempty"`
	Images            []kustomizeImage  `yaml:"images"`
	Replicas          []kustomizeCount  `yaml:"replicas"`
}

type kustomizeImage struct {
	Name   string `yaml:"name"`
	NewTag string `yaml:"newTag"`
}

type kustomizeCount struct {
	Name  string `yaml:"name"`
	Count int    `yaml:"count"`
}

const kustomizationHeader = "# Généré par Symphony : tag de l'image, réplicas et redémarrages du service\n"

func (k kustomization) render() (string, error) {
	var b strings.Builder
	b.WriteString(kustomizationHeader)
	enc := yaml.NewEncoder(&b)
	enc.SetIndent(2) // comme les autres manifests (yaml.v3 indente de 4 par défaut)
	if err := enc.Encode(k); err != nil {
		return "", err
	}
	if err := enc.Close(); err != nil {
		return "", err
	}
	return b.String(), nil
}

func parseKustomization(content string) (kustomization, error) {
	var k kustomization
	if err := yaml.Unmarshal([]byte(content), &k); err != nil {
		return k, fmt.Errorf("%s: %w", kustomizationFile, err)
	}
	if len(k.Replicas) != 1 {
		return k, fmt.Errorf("%s: un seul bloc replicas attendu", kustomizationFile)
	}
	return k, nil
}

// files rend les manifests du service, chemins relatifs à son dossier
func (s service) files() (map[string]string, error) {
	files := make(map[string]string, 4)
	resources := []string{deploymentFile, serviceFile}
	for file, tmpl := range map[string]*template.Template{deploymentFile: deploymentTmpl, serviceFile: serviceTmpl} {
		var b strings.Builder
		if err := tmpl.Execute(&b, s); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		files[file] = b.String()
	}
	if s.IngressHost != "" {
		var b strings.Builder
		if err := ingressTmpl.Execute(&b, s); err != nil {
			return nil, fmt.Errorf("%s: %w", ingressFile, err)
		}
		files[ingressFile] = b.String()
		resources = append(resources, ingressFile)
	}

	k, err := kustomization{
		APIVersion: "kustomize.config.k8s.io/v1beta1",
		Kind:       "Kustomization",
		Resources:  resources,
		Images:     []kustomizeImage{{Name: s.Image, NewTag: s.Tag}},
		Replicas:   []kustomizeCount{{Name: s.Name, Count: 1}},
	}.render()
	if err != nil {
		return nil, err
	}
	files[kustomizationFile] = k
	return files, nil
}

// q met une valeur entre guillemets : une chaîne JSON est une chaîne YAML
// valide, quel que soit son contenu (« : », « # », retours à la ligne…)
func q(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

var funcs = template.FuncMap{"q": q}

var deploymentTmpl = template.Must(template.New(deploymentFile).Funcs(funcs).Parse(`# Généré par Symphony
apiVersion: apps/v1
kind: Deployment
metadata:
  name: {{.Name}}
  labels:
    app.kubernetes.io/name: {{.Name}}
    app.kubernetes.io/managed-by: symphony
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: {{.Name}}
  template:
    metadata:
      labels:
        app.kubernetes.io/name: {{.Name}}
    spec:
      containers:
        - name: app
          image: {{q .Image}}
          ports:
            - name: http
              containerPort: {{.Port}}
{{- if .Env}}
          env:
{{- range .Env}}
            - name: {{q .Name}}
              value: {{q .Value}}
{{- end}}
{{- end}}
          readinessProbe:
            tcpSocket:
              port: http
          resources:
            requests:
              cpu: 10m
              memory: 32Mi
`))

var serviceTmpl = template.Must(template.New(serviceFile).Funcs(funcs).Parse(`# Généré par Symphony
apiVersion: v1
kind: Service
metadata:
  name: {{.Name}}
  labels:
    app.kubernetes.io/name: {{.Name}}
    app.kubernetes.io/managed-by: symphony
spec:
  selector:
    app.kubernetes.io/name: {{.Name}}
  ports:
    - name: http
      port: 80
      targetPort: http
`))

var ingressTmpl = template.Must(template.New(ingressFile).Funcs(funcs).Parse(`# Généré par Symphony
apiVersion: networking.k8s.io/v1
kind: Ingress
metadata:
  name: {{.Name}}
  labels:
    app.kubernetes.io/name: {{.Name}}
    app.kubernetes.io/managed-by: symphony
spec:
{{- if .IngressClass}}
  ingressClassName: {{q .IngressClass}}
{{- end}}
  rules:
    - host: {{q .IngressHost}}
      http:
        paths:
          - path: /
            pathType: Prefix
            backend:
              service:
                name: {{.Name}}
                port:
                  name: http
`))
