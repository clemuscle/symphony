// Package argocd est le runtime Kubernetes de Symphony, en GitOps : les
// manifests de chaque service sont écrits dans un repo de déploiement, séparé
// du code, et Argo CD les applique au cluster. Symphony ne parle jamais à
// l'API Kubernetes : il commite, crée l'Application Argo CD et lit son état.
package argocd

import (
	"bytes"
	"crypto/tls"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
)

// Config : réglages du runtime (section config du provider)
type Config struct {
	URL      string // API Argo CD, ex. http://argocd-server.argocd
	Token    string // jeton d'un compte Argo CD (apiKey)
	Insecure bool   // TLS sans vérification du certificat (Argo CD en local)

	Repo    string // repo de déploiement, tel que le SCM le nomme (root/symphony-deployments)
	Branch  string
	Path    string // dossier des services dans ce repo
	RepoURL string // URL de clone du même repo, vue par Argo CD

	Project   string // projet Argo CD
	Cluster   string // API du cluster cible, vue par Argo CD
	Namespace string // namespace Kubernetes des services

	IngressDomain string // service joignable sur <nom>.<domaine> ; vide : pas d'Ingress
	IngressClass  string // vide : classe par défaut du cluster
	IngressPort   string // port public de l'Ingress, s'il n'est pas 80
}

// Labels des Applications : celles de Symphony (seules listées), arrêtées
const (
	managedLabel   = "app.kubernetes.io/managed-by"
	managedValue   = "symphony"
	stoppedLabel   = "symphony/stopped"
	resourcesFinal = "resources-finalizer.argocd.argoproj.io" // supprimer l'Application supprime ses ressources
)

type Provider struct {
	id     string
	scm    provider.SCMProvider // écrit dans le repo de déploiement
	cfg    Config
	client *http.Client
}

// New crée le runtime. scm est le provider du même jeu de configuration que
// ce runtime : ils sont remplacés ensemble au rechargement.
func New(id string, scm provider.SCMProvider, cfg Config) (*Provider, error) {
	if cfg.URL == "" || cfg.Repo == "" || cfg.RepoURL == "" {
		return nil, fmt.Errorf("url, repo et repo_url requis")
	}
	if cfg.Branch == "" {
		cfg.Branch = "main"
	}
	cfg.Path = strings.Trim(cfg.Path, "/")
	if cfg.Project == "" {
		cfg.Project = "default"
	}
	if cfg.Cluster == "" {
		cfg.Cluster = "https://kubernetes.default.svc"
	}
	if cfg.Namespace == "" {
		cfg.Namespace = "default"
	}
	cfg.URL = strings.TrimRight(cfg.URL, "/")
	transport := http.DefaultTransport.(*http.Transport).Clone()
	if cfg.Insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}
	return &Provider{
		id:     id,
		scm:    scm,
		cfg:    cfg,
		client: &http.Client{Timeout: 30 * time.Second, Transport: transport},
	}, nil
}

func (p *Provider) ID() string   { return p.id }
func (p *Provider) Type() string { return "argocd" }

// ─── Déploiement : commit dans le repo de déploiement, puis Application ─────

func (p *Provider) Deploy(req provider.DeployRequest) (*provider.Deployment, error) {
	if err := validName(req.Name); err != nil {
		return nil, err
	}
	// Les identifiants de registry ne vont jamais dans Git : le cluster tire
	// les images avec les siens (registries.yaml de k3s, imagePullSecret…)
	image, tag := provider.SplitImage(req.Image, req.Tag)
	port := req.Port
	if port == 0 {
		port = 8080
	}
	svc := service{
		Name:         req.Name,
		Image:        image,
		Tag:          tag,
		Port:         port,
		Env:          sortedEnv(req.EnvVars),
		IngressClass: p.cfg.IngressClass,
	}
	if p.cfg.IngressDomain != "" {
		svc.IngressHost = req.Name + "." + p.cfg.IngressDomain
	}
	files, err := svc.files()
	if err != nil {
		return nil, err
	}
	if err := p.commit(req.Name, files, fmt.Sprintf("deploy: %s %s:%s [symphony]", req.Name, image, tag)); err != nil {
		return nil, err
	}
	if err := p.upsertApplication(req.Name, false); err != nil {
		return nil, err
	}
	p.refresh(req.Name)
	return p.Status(req.Name)
}

// Stop met le service à zéro réplica, dans Git : Argo CD l'applique
func (p *Provider) Stop(id string) error {
	return p.update(id, true, func(k *kustomization) {
		k.Replicas[0].Count = 0
	})
}

// Restart relance les pods (et rallume un service arrêté), dans Git
func (p *Provider) Restart(id string) error {
	return p.update(id, false, func(k *kustomization) {
		k.Replicas[0].Count = 1
		if k.CommonAnnotations == nil {
			k.CommonAnnotations = map[string]string{}
		}
		k.CommonAnnotations[restartAnnotation] = time.Now().UTC().Format(time.RFC3339)
	})
}

// update modifie kustomization.yaml du service, commite, puis met à jour
// l'Application (marquée arrêtée ou non)
func (p *Provider) update(id string, stopped bool, change func(*kustomization)) error {
	if err := validName(id); err != nil {
		return err
	}
	file := p.servicePath(id, kustomizationFile)
	content, err := p.scm.GetFileContent(p.cfg.Repo, p.cfg.Branch, file)
	if errors.Is(err, provider.ErrNotFound) {
		return fmt.Errorf("déploiement %s introuvable dans %s: %w", id, p.cfg.Repo, provider.ErrNotFound)
	}
	if err != nil {
		return fmt.Errorf("lecture %s: %w", file, err)
	}
	k, err := parseKustomization(content)
	if err != nil {
		return err
	}
	change(&k)
	rendered, err := k.render()
	if err != nil {
		return err
	}
	verb := "restart"
	if stopped {
		verb = "stop"
	}
	if err := p.commit(id, map[string]string{kustomizationFile: rendered}, fmt.Sprintf("%s: %s [symphony]", verb, id)); err != nil {
		return err
	}
	if err := p.upsertApplication(id, stopped); err != nil {
		return err
	}
	p.refresh(id)
	return nil
}

func (p *Provider) servicePath(name, file string) string {
	return path.Join(p.cfg.Path, name, file)
}

// commit pousse les fichiers du service en un seul commit
func (p *Provider) commit(name string, files map[string]string, msg string) error {
	full := make(map[string]string, len(files))
	for file, content := range files {
		full[p.servicePath(name, file)] = content
	}
	if err := p.scm.PushFiles(p.cfg.Repo, p.cfg.Branch, full, msg); err != nil {
		return fmt.Errorf("commit dans %s: %w", p.cfg.Repo, err)
	}
	return nil
}

// ─── API Argo CD ─────────────────────────────────────────────────────────────

// application : la partie d'une Application Argo CD que Symphony écrit et lit
type application struct {
	Metadata struct {
		Name              string            `json:"name"`
		Labels            map[string]string `json:"labels,omitempty"`
		Finalizers        []string          `json:"finalizers,omitempty"`
		CreationTimestamp time.Time         `json:"creationTimestamp,omitempty"`
	} `json:"metadata"`
	Spec struct {
		Project string `json:"project"`
		Source  struct {
			RepoURL        string `json:"repoURL"`
			Path           string `json:"path"`
			TargetRevision string `json:"targetRevision"`
		} `json:"source"`
		Destination struct {
			Server    string `json:"server"`
			Namespace string `json:"namespace"`
		} `json:"destination"`
		SyncPolicy struct {
			Automated struct {
				Prune    bool `json:"prune"`
				SelfHeal bool `json:"selfHeal"`
			} `json:"automated"`
			SyncOptions []string `json:"syncOptions,omitempty"`
		} `json:"syncPolicy"`
	} `json:"spec"`
	Status *appStatus `json:"status,omitempty"` // lu seulement
}

type appStatus struct {
	Sync struct {
		Status string `json:"status"`
	} `json:"sync"`
	Health struct {
		Status string `json:"status"`
	} `json:"health"`
	Summary struct {
		Images []string `json:"images"`
	} `json:"summary"`
}

// upsertApplication crée l'Application du service, ou la met à jour si sa
// définition a changé (configuration du runtime, arrêt). Synchronisation
// automatique : Argo CD applique chaque commit et corrige toute dérive.
func (p *Provider) upsertApplication(name string, stopped bool) error {
	var app application
	app.Metadata.Name = name
	// Toujours écrit, à true ou false : l'upsert d'Argo CD fusionne les
	// labels, un label retiré resterait en place
	app.Metadata.Labels = map[string]string{managedLabel: managedValue, stoppedLabel: fmt.Sprint(stopped)}
	app.Metadata.Finalizers = []string{resourcesFinal}
	app.Spec.Project = p.cfg.Project
	app.Spec.Source.RepoURL = p.cfg.RepoURL
	app.Spec.Source.Path = path.Join(p.cfg.Path, name)
	app.Spec.Source.TargetRevision = p.cfg.Branch
	app.Spec.Destination.Server = p.cfg.Cluster
	app.Spec.Destination.Namespace = p.cfg.Namespace
	app.Spec.SyncPolicy.Automated.Prune = true
	app.Spec.SyncPolicy.Automated.SelfHeal = true
	app.Spec.SyncPolicy.SyncOptions = []string{"CreateNamespace=true"}

	data, status, err := p.argo("POST", "/api/v1/applications?upsert=true", app)
	if err != nil {
		return err
	}
	if status != http.StatusOK {
		return apiError("création de l'Application", status, data)
	}
	return nil
}

// refresh demande à Argo CD de relire Git tout de suite, sans attendre son
// intervalle de scrutation (3 min) : le commit est appliqué en quelques
// secondes. Un échec n'est pas bloquant, la scrutation finira par le voir.
func (p *Provider) refresh(name string) {
	p.argo("GET", "/api/v1/applications/"+url.PathEscape(name)+"?refresh=normal", nil)
}

func (p *Provider) Status(id string) (*provider.Deployment, error) {
	data, status, err := p.argo("GET", "/api/v1/applications/"+url.PathEscape(id), nil)
	if err != nil {
		return nil, err
	}
	// Argo CD répond 403 plutôt que 404 quand l'Application n'existe pas
	// (pour ne pas révéler ce que le compte ne peut pas voir)
	if status == http.StatusNotFound || status == http.StatusForbidden {
		return nil, fmt.Errorf("application %s: %w", id, provider.ErrNotFound)
	}
	if status != http.StatusOK {
		return nil, apiError("état de l'Application", status, data)
	}
	var app application
	if err := json.Unmarshal(data, &app); err != nil {
		return nil, fmt.Errorf("argocd: réponse illisible: %w", err)
	}
	d := p.deployment(app)
	return &d, nil
}

func (p *Provider) List() ([]provider.Deployment, error) {
	data, status, err := p.argo("GET", "/api/v1/applications?selector="+url.QueryEscape(managedLabel+"="+managedValue), nil)
	if err != nil {
		return nil, err
	}
	if status != http.StatusOK {
		return nil, apiError("liste des Applications", status, data)
	}
	var list struct {
		Items []application `json:"items"`
	}
	if err := json.Unmarshal(data, &list); err != nil {
		return nil, fmt.Errorf("argocd: réponse illisible: %w", err)
	}
	deployments := make([]provider.Deployment, 0, len(list.Items))
	for _, app := range list.Items {
		deployments = append(deployments, p.deployment(app))
	}
	return deployments, nil
}

// Logs lit les journaux des pods du service via Argo CD (pas d'accès direct
// au cluster). La réponse est un flux d'objets JSON, un par ligne de journal.
func (p *Provider) Logs(id string, lines int) (string, error) {
	q := url.Values{
		"namespace":    {p.cfg.Namespace},
		"kind":         {"Deployment"},
		"group":        {"apps"},
		"resourceName": {id},
		"tailLines":    {fmt.Sprint(lines)},
		"follow":       {"false"},
	}
	data, status, err := p.argo("GET", "/api/v1/applications/"+url.PathEscape(id)+"/logs?"+q.Encode(), nil)
	if err != nil {
		return "", err
	}
	if status != http.StatusOK {
		return "", apiError("journaux", status, data)
	}
	var b strings.Builder
	dec := json.NewDecoder(bytes.NewReader(data))
	for {
		var entry struct {
			Result struct {
				Content string `json:"content"`
				Last    bool   `json:"last"`
			} `json:"result"`
			Error *struct {
				Message string `json:"message"`
			} `json:"error"`
		}
		if err := dec.Decode(&entry); err == io.EOF {
			break
		} else if err != nil {
			return "", fmt.Errorf("argocd: journaux illisibles: %w", err)
		}
		if entry.Error != nil {
			return "", fmt.Errorf("argocd: journaux: %s", entry.Error.Message)
		}
		if entry.Result.Last {
			break
		}
		b.WriteString(entry.Result.Content)
		b.WriteByte('\n')
	}
	return b.String(), nil
}

// deployment traduit l'état d'une Application
func (p *Provider) deployment(app application) provider.Deployment {
	st := app.Status
	if st == nil {
		st = &appStatus{}
	}
	d := provider.Deployment{
		ID:        app.Metadata.Name,
		Name:      app.Metadata.Name,
		Status:    deploymentStatus(st.Health.Status, app.Metadata.Labels[stoppedLabel] == "true"),
		CreatedAt: app.Metadata.CreationTimestamp,
		Sync:      st.Sync.Status,
		Health:    st.Health.Status,
	}
	if len(st.Summary.Images) > 0 {
		d.Image = st.Summary.Images[0]
	}
	if p.cfg.IngressDomain != "" {
		host := app.Metadata.Name + "." + p.cfg.IngressDomain
		d.Port = 80
		if p.cfg.IngressPort != "" && p.cfg.IngressPort != "80" {
			host += ":" + p.cfg.IngressPort
			fmt.Sscan(p.cfg.IngressPort, &d.Port)
		}
		d.URL = "http://" + host
	}
	return d
}

// deploymentStatus : santé Argo CD → statut commun aux runtimes
func deploymentStatus(health string, stopped bool) string {
	switch {
	case stopped:
		return "stopped"
	case health == "Healthy":
		return "running"
	case health == "Progressing", health == "Missing", health == "":
		return "deploying" // Missing : ressources pas encore créées
	case health == "Suspended":
		return "stopped"
	default:
		return "error" // Degraded, Unknown
	}
}

func (p *Provider) argo(method, apiPath string, body any) ([]byte, int, error) {
	var buf io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, 0, err
		}
		buf = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, p.cfg.URL+apiPath, buf)
	if err != nil {
		return nil, 0, err
	}
	req.Header.Set("Authorization", "Bearer "+p.cfg.Token)
	req.Header.Set("Content-Type", "application/json")
	resp, err := p.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("argocd: %w", err)
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	return data, resp.StatusCode, err
}

// apiError reprend le message d'erreur d'Argo CD ({"error": ..., "message": ...})
func apiError(op string, status int, data []byte) error {
	var e struct {
		Message string `json:"message"`
	}
	if json.Unmarshal(data, &e) == nil && e.Message != "" {
		return fmt.Errorf("argocd: %s: status %d — %s", op, status, e.Message)
	}
	return fmt.Errorf("argocd: %s: status %d — %s", op, status, strings.TrimSpace(string(data)))
}

var _ provider.RuntimeProvider = (*Provider)(nil)
