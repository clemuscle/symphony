package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"path"
	"time"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
)

// ─── Créer un projet ─────────────────────────────────────────────────────────

// createParams : demande validée par CreateProject
type createParams struct {
	CreateProjectRequest
	SCMProvider string `json:"scm_id"`
	CIProvider  string `json:"ci_id"`
}

// createRepo récupère le skeleton et le template CI, crée le repo et y
// pousse le tout en un commit (qui lance la première pipeline). Rejouée après
// un redémarrage, elle ne recrée pas un repo déjà créé.
func (d TaskDeps) createRepo(_ context.Context, x *tasks.Exec) error {
	var p createParams
	if err := x.Params(&p); err != nil {
		return err
	}
	var repo provider.Repo
	var pushedAt time.Time
	created := x.Get("repo", &repo)
	if created && x.Get("pushed_at", &pushedAt) {
		return nil
	}
	stack, err := d.Stacks.GetStack(p.Stack)
	if err != nil {
		return err
	}
	scm, err := d.Providers.SCM(p.SCMProvider)
	if err != nil {
		return err
	}
	ci, err := d.Providers.CI(p.CIProvider)
	if err != nil {
		return err
	}

	// Lus avant de créer le repo : une stack cassée ne laisse pas de repo vide
	x.Progress("lecture de la stack %s", stack.ID)
	files, err := scaffoldFiles(d.Stacks, stack, ci, p.Name, p.Description, p.Port, p.Parameters)
	if err != nil {
		return err
	}
	if !created {
		x.Progress("création du repo")
		r, err := scm.CreateRepo(provider.CreateRepoRequest{
			Name:        p.Name,
			Description: p.Description,
			Namespace:   p.Namespace,
			Private:     true,
		})
		if err != nil {
			return fmt.Errorf("création du repo: %w", err)
		}
		repo = *r
		x.Set("repo", repo)
		x.Set("pipelines_url", ci.PipelinesURL(repo.WebURL))
	}

	// La fiche catalogue vit dans le repo : elle dépend de son URL
	file := d.Syncer.File()
	fiche, err := catalogueFile(files[file], p, stack, &repo, ci, x.Task().CreatedBy,
		stackKubernetes(stack, files), stack.InfraKept(p.Parameters))
	if err != nil {
		return err
	}
	files[file] = fiche

	// Un seul commit : il lance la première pipeline, et sur GitLab un premier
	// commit sans .gitlab-ci.yml déclencherait l'Auto DevOps
	x.Progress("push du squelette et de la CI (%d fichiers)", len(files))
	pushedAt = time.Now()
	if err := scm.PushFiles(repo.Path, repo.DefaultBranch, files, "chore: scaffold initial [symphony]"); err != nil {
		return fmt.Errorf("push du squelette: %w", err)
	}
	x.Set("pushed_at", pushedAt)
	return nil
}

// scaffoldFiles : squelette et CI de la stack, variables remplacées ; les
// fichiers dont la condition (spec.files) n'est pas remplie sont omis
func scaffoldFiles(stacks *gitops.StackLoader, stack *gitops.Stack, ci provider.CIProvider,
	name, description string, port int, params map[string]any) (map[string]string, error) {
	skeleton, err := stacks.GetScaffoldFiles(*stack)
	if err != nil {
		return nil, fmt.Errorf("skeleton: %w", err)
	}
	files := make(map[string]string, len(skeleton)+1)
	for file, content := range skeleton {
		if stack.Keep(file, params) {
			files[file] = replacePlaceholders(content, name, description, stack, port, params)
		}
	}
	ciContent, err := stacks.GetCITemplate(*stack)
	if err != nil {
		return nil, fmt.Errorf("template CI: %w", err)
	}
	files[ci.ConfigFile()] = replacePlaceholders(ciContent, name, description, stack, port, params)
	return files, nil
}

// stackKubernetes : les manifests Kubernetes de la stack, s'ils sont dans
// les fichiers du repo créé (un paramètre peut les omettre)
func stackKubernetes(stack *gitops.Stack, files map[string]string) *catalogue.Kubernetes {
	k := stack.Kubernetes
	switch {
	case k == nil:
		return nil
	case k.Helm != "" && files[path.Join(k.Helm, "Chart.yaml")] != "":
		return &catalogue.Kubernetes{Helm: k.Helm}
	case k.Kustomize != "" && files[path.Join(k.Kustomize, "kustomization.yaml")] != "":
		return &catalogue.Kubernetes{Kustomize: k.Kustomize}
	}
	return nil
}

// registerProject enregistre le projet et abonne Symphony aux pipelines du
// repo. Le webhook n'est pas bloquant : le projet existe, un échec est
// signalé dans les résultats.
func (d TaskDeps) registerProject(_ context.Context, x *tasks.Exec) error {
	var p createParams
	if err := x.Params(&p); err != nil {
		return err
	}
	var repo provider.Repo
	if !x.Get("repo", &repo) {
		return errors.New("repo absent des résultats de l'étape précédente")
	}
	stack, err := d.Stacks.GetStack(p.Stack)
	if err != nil {
		return err
	}
	ci, err := d.Providers.CI(p.CIProvider)
	if err != nil {
		return err
	}

	proj, err := d.DB.GetProjectByName(p.Name)
	if errors.Is(err, sql.ErrNoRows) {
		// Registry : facultatif, un projet sans registry reste déployable avec
		// une image saisie à la main
		regProvider, registryURL := stack.RegProvider, ""
		if reg, err := d.Providers.GetRegistry(stack.RegProvider); err == nil {
			regProvider, registryURL = reg.ID(), reg.GetImageURL(repo.Path)
		}
		rtProvider := p.RuntimeProvider
		if rtProvider == "" {
			rtProvider = stack.RTProvider
		}
		proj = &database.Project{
			Name:            p.Name,
			Description:     p.Description,
			Language:        stack.Language,
			Type:            stack.Type,
			Port:            p.Port,
			Namespace:       p.Namespace,
			SCMProvider:     p.SCMProvider,
			CIProvider:      ci.ID(),
			RegProvider:     regProvider,
			RTProvider:      rtProvider,
			RepoPath:        repo.Path,
			RepoURL:         repo.HTTPCloneURL,
			RegistryURL:     registryURL,
			Stack:           stack.ID,
			StackParameters: p.Parameters,
			Team:            p.Owner,
			CreatedBy:       x.Task().CreatedBy,
		}
		if err := d.DB.CreateProject(proj); err != nil {
			return fmt.Errorf("enregistrement du projet: %w", err)
		}
	} else if err != nil {
		return fmt.Errorf("db: %w", err)
	}
	x.Set("registry_url", proj.RegistryURL)

	// La fiche est dans le repo depuis le premier commit : le catalogue la lit
	// dès que le projet est en base
	go d.Syncer.ForceSync()

	if s := d.Webhooks.Get(); s.Enabled() {
		x.Progress("webhook de pipeline")
		scm, err := d.Providers.SCM(p.SCMProvider)
		if err == nil {
			err = scm.EnsureWebhook(repo.Path, s.Endpoint(scm.Platform()), s.Secret)
		}
		if err != nil {
			slog.Warn("webhook de pipeline", "project", p.Name, "error", err)
			x.Set("webhook", "sondage seulement : "+err.Error())
		} else {
			x.Set("webhook", "ok")
		}
	} else {
		x.Set("webhook", "désactivé (sondage)")
	}
	return nil
}

// waitFirstPipeline attend la pipeline lancée par le push du squelette : à sa
// fin, l'image du service est dans le registry
func (d TaskDeps) waitFirstPipeline(_ context.Context, x *tasks.Exec) error {
	var p createParams
	if err := x.Params(&p); err != nil {
		return err
	}
	var repo provider.Repo
	var pushedAt time.Time
	if !x.Get("repo", &repo) || !x.Get("pushed_at", &pushedAt) {
		return errors.New("repo absent des résultats de l'étape précédente")
	}
	var pipelinesURL string
	x.Get("pipelines_url", &pipelinesURL)
	x.WaitPipeline(p.CIProvider, repo.Path, "", pipelinesURL, pushedAt)
	return nil
}
