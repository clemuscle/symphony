package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
)

// ─── Déployer ────────────────────────────────────────────────────────────────

// deployParams : demande validée (Deploy, Promote, Rollback). Pas de
// variables ici : elles sont lues dans la fiche au moment du déploiement, et
// les secrets résolus à ce moment-là, jamais enregistrés.
type deployParams struct {
	ProjectName  string `json:"project_name"`
	Environment  string `json:"environment,omitempty"`
	Image        string `json:"image"`
	Tag          string `json:"tag"`
	Port         int    `json:"port"`
	HostPort     int    `json:"host_port"`
	Runtime      string `json:"runtime"`
	Origin       string `json:"origin,omitempty"` // deploy, promotion, rollback
	PromotedFrom string `json:"promoted_from,omitempty"`
	// Approval : l'environnement demande l'accord d'un lead, et le
	// demandeur n'en est pas un
	Approval bool `json:"approval,omitempty"`
}

// deployTarget : résultat « target » d'une tâche de déploiement, lu par
// l'activité et les approbations
type deployTarget struct {
	Environment  string `json:"environment,omitempty"`
	Image        string `json:"image"`
	Origin       string `json:"origin,omitempty"`
	PromotedFrom string `json:"promoted_from,omitempty"`
	Approval     bool   `json:"approval,omitempty"`
}

// awaitApproval : première étape d'un déploiement. Sans approbation requise,
// elle passe aussitôt ; sinon la tâche attend qu'un lead de l'équipe (ou un
// admin) l'approuve ou la refuse (Runner.Decide).
func (d TaskDeps) awaitApproval(_ context.Context, x *tasks.Exec) error {
	var p deployParams
	if err := x.Params(&p); err != nil {
		return err
	}
	var target deployTarget
	if !x.Get("target", &target) {
		x.Set("target", deployTarget{
			Environment:  p.Environment,
			Image:        p.Image + ":" + orLatest(p.Tag),
			Origin:       p.Origin,
			PromotedFrom: p.PromotedFrom,
			Approval:     p.Approval,
		})
	}
	var decision tasks.Decision
	if p.Approval && !x.Get("approval", &decision) {
		x.WaitApproval()
	}
	return nil
}

func (d TaskDeps) deploy(_ context.Context, x *tasks.Exec) error {
	var p deployParams
	if err := x.Params(&p); err != nil {
		return err
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
	var decision tasks.Decision
	x.Get("approval", &decision)
	release := &database.Deployment{
		ProjectName:  p.ProjectName,
		Environment:  p.Environment,
		DeploymentID: deployment.ID,
		RTProvider:   rt.ID(),
		Image:        p.Image,
		Tag:          p.Tag,
		Port:         deployment.Port,
		Status:       database.ReleaseDeploying,
		URL:          deployment.URL,
		Origin:       p.Origin,
		PromotedFrom: p.PromotedFrom,
		DeployedBy:   x.Task().CreatedBy,
		ApprovedBy:   decision.By,
		TaskID:       x.Task().ID,
	}
	if err := d.DB.CreateDeployment(release); err != nil {
		return fmt.Errorf("historique des déploiements : %w", err)
	}
	x.Set("release_id", release.ID)
	x.Set("deployment", deployment)
	return nil
}

// waitRunning attend que le service tourne : immédiat pour Docker (la
// bascule a déjà vérifié le démarrage), le temps de la synchronisation et des
// pods pour Argo CD. La version de l'historique passe à running, ou failed.
func (d TaskDeps) waitRunning(ctx context.Context, x *tasks.Exec) (err error) {
	var releaseID int
	if x.Get("release_id", &releaseID) {
		defer func() {
			status := database.ReleaseRunning
			if err != nil {
				if ctx.Err() != nil {
					return // arrêt de Symphony : l'étape reprendra
				}
				status = database.ReleaseFailed
			}
			if serr := d.DB.SetReleaseStatus(releaseID, status); serr != nil {
				slog.Warn("historique des déploiements", "release", releaseID, "error", serr)
			}
		}()
	}
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
			return nil
		case "error", "exited", "dead":
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
	list, err := scm.ListFiles(proj.RepoPath, proj.Branch, dir)
	if err != nil {
		return nil, fmt.Errorf("%s du repo %s : %w", dir, proj.RepoPath, err)
	}
	files := make(map[string]string, len(list))
	for _, f := range list {
		content, err := scm.GetFileContent(proj.RepoPath, proj.Branch, f)
		if err != nil {
			return nil, fmt.Errorf("lecture de %s : %w", f, err)
		}
		files[strings.TrimPrefix(f, dir+"/")] = content
	}
	return files, nil
}
