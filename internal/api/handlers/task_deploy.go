package handlers

import (
	"context"
	"errors"
	"fmt"
	"path"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
)

// ─── Déployer ────────────────────────────────────────────────────────────────

// deployParams : demande validée (Deploy, Promote). Pas de variables ici :
// elles sont lues dans la fiche au moment du déploiement, et les secrets
// résolus à ce moment-là, jamais enregistrés.
type deployParams struct {
	ProjectName  string `json:"project_name"`
	Environment  string `json:"environment,omitempty"`
	Image        string `json:"image"`
	Tag          string `json:"tag"`
	Port         int    `json:"port"`
	HostPort     int    `json:"host_port"`
	Runtime      string `json:"runtime"`
	PromotedFrom string `json:"promoted_from,omitempty"`
}

// deployTarget : résultat « target » d'une tâche de déploiement
type deployTarget struct {
	Environment  string `json:"environment,omitempty"`
	PromotedFrom string `json:"promoted_from,omitempty"`
}

func (d TaskDeps) deploy(_ context.Context, x *tasks.Exec) error {
	var p deployParams
	if err := x.Params(&p); err != nil {
		return err
	}
	// Cible lue par l'activité (environnement, promotion), dès le début
	var target deployTarget
	if !x.Get("target", &target) {
		x.Set("target", deployTarget{Environment: p.Environment, PromotedFrom: p.PromotedFrom})
	}
	var done provider.Deployment
	if x.Get("deployment", &done) {
		return nil
	}
	rt, err := d.Providers.Runtime(p.Runtime)
	if err != nil {
		return err
	}
	env, secretEnv, err := d.appEnvVars(p.ProjectName, p.Environment, rt.SecretRefs())
	if err != nil {
		return err
	}
	manifests, err := d.stackManifests(p.ProjectName, rt)
	if err != nil {
		return err
	}
	infra, err := d.stackInfra(p.ProjectName)
	if err != nil {
		return err
	}
	if p.PromotedFrom != "" {
		x.Progress("promotion %s → %s, %s : %s:%s", p.PromotedFrom, p.Environment, rt.ID(), p.Image, orLatest(p.Tag))
	} else {
		x.Progress("%s : %s:%s", rt.ID(), p.Image, orLatest(p.Tag))
	}
	deployment, err := rt.Deploy(provider.DeployRequest{
		Name:        provider.DeploymentName(p.ProjectName, p.Environment),
		Project:     p.ProjectName,
		Environment: p.Environment,
		Image:       p.Image,
		Tag:         p.Tag,
		Port:        p.Port,
		HostPort:    p.HostPort,
		EnvVars:     env,
		SecretEnv:   secretEnv,
		// Identifiants lus au moment du déploiement, jamais enregistrés dans la tâche
		RegistryAuth: registryAuth(d.Providers, d.DB, p.ProjectName, p.Image),
		Manifests:    manifests,
		Infra:        infra,
	})
	if err != nil {
		return err
	}
	deployment.Provider = rt.ID()
	d.DB.CreateDeployment(&database.Deployment{
		ProjectName:  p.ProjectName,
		DeploymentID: deployment.ID,
		RTProvider:   rt.ID(),
		Image:        p.Image,
		Tag:          p.Tag,
		Port:         deployment.Port,
		Status:       deployment.Status,
		URL:          deployment.URL,
		DeployedBy:   x.Task().CreatedBy,
	})
	x.Set("deployment", deployment)
	return nil
}

// waitRunning attend que le service tourne : immédiat pour Docker (la
// bascule a déjà vérifié le démarrage), le temps de la synchronisation et des
// pods pour Argo CD
func (d TaskDeps) waitRunning(ctx context.Context, x *tasks.Exec) error {
	var p deployParams
	if err := x.Params(&p); err != nil {
		return err
	}
	var dep provider.Deployment
	if !x.Get("deployment", &dep) {
		return errors.New("déploiement absent des résultats de l'étape précédente")
	}
	rt, err := d.Providers.Runtime(p.Runtime)
	if err != nil {
		return err
	}
	deadline := time.Now().Add(startupTimeout)
	for {
		st, err := rt.Status(dep.ID)
		if err != nil {
			return fmt.Errorf("état du déploiement: %w", err)
		}
		if st.Sync != "" || st.Health != "" {
			x.Progress("%s · Argo CD : %s / %s", st.Status, st.Sync, st.Health)
		} else {
			x.Progress("%s", st.Status)
		}
		switch st.Status {
		case "running":
			st.Provider = rt.ID()
			x.Set("deployment", st)
			d.DB.UpdateDeploymentStatus(st.ID, st.Status)
			return nil
		case "error", "exited", "dead":
			d.DB.UpdateDeploymentStatus(st.ID, st.Status)
			return fmt.Errorf("le service ne démarre pas (%s)", x.Task().Detail)
		}
		if time.Now().After(deadline) {
			return fmt.Errorf("service toujours %s après %s", st.Status, startupTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(2 * time.Second):
		}
	}
}

func orLatest(tag string) string {
	if tag == "" {
		return "latest"
	}
	return tag
}

// appEnvVars : variables de l'application dans l'environnement env (fiche du
// projet). Secrets : références si le runtime les transmet lui-même (byRef),
// sinon résolus ici. Valeurs jamais enregistrées dans la tâche.
func (d TaskDeps) appEnvVars(project, env string, byRef bool) (map[string]string, map[string]provider.SecretRef, error) {
	plain, secrets, err := appEnv(d.Store, project, env)
	if err != nil {
		return nil, nil, err
	}
	if byRef {
		return plain, secrets, nil
	}
	for k, ref := range secrets {
		v, err := provider.ResolveSecret(ref.String(), d.Providers.Secrets)
		if err != nil {
			return nil, nil, fmt.Errorf("variable %s : %w", k, err)
		}
		plain[k] = v
	}
	return plain, nil, nil
}

// stackManifests : le chart Helm ou la base Kustomize que la fiche du projet
// déclare (spec.kubernetes, écrit par sa stack), lus sur main dans son repo.
// Seul le runtime argocd s'en sert ; nil : il génère ses manifests.
func (d TaskDeps) stackManifests(project string, rt provider.RuntimeProvider) (*provider.Manifests, error) {
	if rt.Type() != "argocd" {
		return nil, nil
	}
	svc, ok := d.Store.Get(project)
	if !ok || svc.Spec.Kubernetes == nil {
		return nil, nil
	}
	m := &provider.Manifests{Kind: "helm", Files: map[string]string{}}
	dir := svc.Spec.Kubernetes.Helm
	if dir == "" {
		m.Kind, dir = "kustomize", svc.Spec.Kubernetes.Kustomize
	}
	dir = strings.Trim(path.Clean(dir), "/")
	if dir == "" || dir == "." || strings.HasPrefix(dir, "..") {
		return nil, fmt.Errorf("fiche du projet : spec.kubernetes %q invalide", dir)
	}
	files, err := d.repoDir(project, dir)
	if err != nil {
		return nil, fmt.Errorf("manifests %s : %w", m.Kind, err)
	}
	m.Files = files
	return m, nil
}

// stackInfra : l'infra que la fiche du projet déclare (spec.infra, écrite par
// sa stack), lue sur main dans son repo. Le runtime l'applique, ou refuse
// s'il ne sait pas (docker) : le service n'irait pas sans sa base.
func (d TaskDeps) stackInfra(project string) (*provider.Infra, error) {
	svc, ok := d.Store.Get(project)
	if !ok || svc.Spec.Infra == nil {
		return nil, nil
	}
	spec := *svc.Spec.Infra
	if err := gitops.CheckInfra(&spec); err != nil {
		return nil, fmt.Errorf("fiche du projet : spec.infra : %w", err)
	}
	files, err := d.repoDir(project, spec.Kubernetes)
	if err != nil {
		return nil, fmt.Errorf("infra : %w", err)
	}
	in := &provider.Infra{Files: files, Env: map[string]provider.InfraSecretRef{}}
	for name, ref := range spec.Env {
		in.Env[name] = provider.InfraSecretRef{Secret: ref.Secret, Key: ref.Key}
	}
	return in, nil
}

// repoDir : les fichiers du dossier dir du repo du projet, sur main, par
// chemin relatif à dir
func (d TaskDeps) repoDir(project, dir string) (map[string]string, error) {
	proj, err := d.DB.GetProjectByName(project)
	if err != nil {
		return nil, fmt.Errorf("projet %s : %w", project, err)
	}
	scm, err := d.Providers.SCM(proj.SCMProvider)
	if err != nil {
		return nil, err
	}
	list, err := scm.ListFiles(proj.RepoPath, "main", dir)
	if err != nil {
		return nil, fmt.Errorf("%s du repo %s : %w", dir, proj.RepoPath, err)
	}
	files := make(map[string]string, len(list))
	for _, f := range list {
		content, err := scm.GetFileContent(proj.RepoPath, "main", f)
		if err != nil {
			return nil, fmt.Errorf("lecture de %s : %w", f, err)
		}
		files[strings.TrimPrefix(f, dir+"/")] = content
	}
	return files, nil
}
