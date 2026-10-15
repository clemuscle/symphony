package handlers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"log/slog"
	"time"

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
	gp, err := d.Templates.GetGoldenPath(p.GoldenPathID)
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

	// Lus avant de créer le repo : un template cassé ne laisse pas de repo vide
	x.Progress("lecture du golden path %s", gp.ID)
	files, err := scaffoldFiles(d.Templates, gp, ci, p.Name, p.Description, p.Port)
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
	fiche, err := catalogueFile(files[file], p, gp, &repo, ci, x.Task().CreatedBy)
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

// scaffoldFiles : skeleton et template CI du golden path, placeholders remplacés
func scaffoldFiles(templates *gitops.TemplateLoader, gp *gitops.GoldenPath, ci provider.CIProvider,
	name, description string, port int) (map[string]string, error) {
	skeleton, err := templates.GetScaffoldFiles(*gp)
	if err != nil {
		return nil, fmt.Errorf("skeleton: %w", err)
	}
	files := make(map[string]string, len(skeleton)+1)
	for path, content := range skeleton {
		files[path] = replacePlaceholders(content, name, description, gp, port)
	}
	ciContent, err := templates.GetCITemplate(*gp)
	if err != nil {
		return nil, fmt.Errorf("template CI: %w", err)
	}
	files[ci.ConfigFile()] = replacePlaceholders(ciContent, name, description, gp, port)
	return files, nil
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
	gp, err := d.Templates.GetGoldenPath(p.GoldenPathID)
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
		regProvider, registryURL := gp.RegProvider, ""
		if reg, err := d.Providers.GetRegistry(gp.RegProvider); err == nil {
			regProvider, registryURL = reg.ID(), reg.GetImageURL(repo.Path)
		}
		rtProvider := p.RuntimeProvider
		if rtProvider == "" {
			rtProvider = gp.RTProvider
		}
		proj = &database.Project{
			Name:        p.Name,
			Description: p.Description,
			Language:    gp.Language,
			Type:        gp.Type,
			Port:        p.Port,
			Namespace:   p.Namespace,
			SCMProvider: p.SCMProvider,
			CIProvider:  ci.ID(),
			RegProvider: regProvider,
			RTProvider:  rtProvider,
			RepoPath:    repo.Path,
			RepoURL:     repo.HTTPCloneURL,
			RegistryURL: registryURL,
			GoldenPath:  gp.ID,
			Team:        p.Owner,
			CreatedBy:   x.Task().CreatedBy,
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
