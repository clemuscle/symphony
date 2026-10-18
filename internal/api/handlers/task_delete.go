package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
)

// ─── Supprimer un projet ─────────────────────────────────────────────────────

// deleteParams : ce qu'il faut pour finir la suppression même si le projet
// a déjà disparu de la base
type deleteParams struct {
	Name        string `json:"name"`
	RepoPath    string `json:"repo_path"`
	SCMProvider string `json:"scm_provider"`
	RegProvider string `json:"reg_provider"`
	// KeepRepo : projet découvert, que Symphony n'a pas créé : son repo et
	// ses images restent
	KeepRepo bool `json:"keep_repo,omitempty"`
}

// removeTimeout : délai pour qu'un déploiement supprimé ait disparu (Argo CD
// supprime les ressources du cluster avant l'Application)
var removeTimeout = 3 * time.Minute

// deleteDeployments supprime le service de chaque runtime (conteneurs et
// images Docker, Application Argo CD, ses ressources et ses manifests), attend
// qu'il ait disparu, puis efface ses déploiements de la base. Tous les runtimes
// sont parcourus : celui d'un ancien déploiement a pu changer depuis.
func (d TaskDeps) deleteDeployments(ctx context.Context, x *tasks.Exec) error {
	var p deleteParams
	if err := x.Params(&p); err != nil {
		return err
	}
	var runtimes []provider.RuntimeProvider
	for _, id := range d.Providers.ListRuntimes() {
		rt, err := d.Providers.Runtime(id)
		if err != nil {
			return err
		}
		x.Progress("suppression de %s sur %s", p.Name, id)
		if err := rt.Remove(p.Name); err != nil {
			return fmt.Errorf("runtime %s: %w", id, err)
		}
		runtimes = append(runtimes, rt)
	}
	deadline := time.Now().Add(removeTimeout)
	for _, rt := range runtimes {
		for {
			_, err := rt.Status(p.Name)
			if errors.Is(err, provider.ErrNotFound) {
				break
			}
			if err != nil {
				return fmt.Errorf("runtime %s: %w", rt.ID(), err)
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("runtime %s : %s toujours présent après %s", rt.ID(), p.Name, removeTimeout)
			}
			x.Progress("%s : ressources en cours de suppression", rt.ID())
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(2 * time.Second):
			}
		}
	}
	if err := d.DB.DeleteDeployments(p.Name); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	return nil
}

// deleteRepo supprime le repo et ses images. GitLab refuse tant que la purge
// du registry n'est pas finie : 2 à 3 minutes. Le repo passe en premier : s'il
// échoue, rien d'autre n'est touché et la suppression peut être relancée.
func (d TaskDeps) deleteRepo(_ context.Context, x *tasks.Exec) error {
	var p deleteParams
	if err := x.Params(&p); err != nil {
		return err
	}
	if p.RepoPath == "" {
		return nil
	}
	if p.KeepRepo {
		x.Progress("projet découvert : %s et ses images sont gardés", p.RepoPath)
		return nil
	}
	// Projets antérieurs à la migration 002 : pas de scm_provider, SCM par défaut
	scm, err := d.Providers.SCM(p.SCMProvider)
	if err != nil {
		return err
	}
	x.Progress("purge du registry puis suppression de %s (2 à 3 min sur GitLab)", p.RepoPath)
	if err := scm.DeleteRepo(p.RepoPath); err != nil && !errors.Is(err, provider.ErrNotFound) {
		return fmt.Errorf("suppression du repo: %w", err)
	}
	// Images hors du repo (GHCR) ; projet sans registry : rien à faire
	if p.RegProvider != "" {
		if reg, err := d.Providers.GetRegistry(p.RegProvider); err == nil {
			x.Progress("suppression des images (%s)", reg.ID())
			if err := reg.DeleteImages(p.RepoPath); err != nil {
				return fmt.Errorf("suppression des images: %w", err)
			}
		}
	}
	return nil
}

func (d TaskDeps) deleteCatalogue(_ context.Context, x *tasks.Exec) error {
	var p deleteParams
	if err := x.Params(&p); err != nil {
		return err
	}
	// La fiche est partie avec le repo (ou n'est plus lue : projet découvert)
	d.Syncer.Forget(p.Name)
	return nil
}

func (d TaskDeps) deleteRecord(_ context.Context, x *tasks.Exec) error {
	var p deleteParams
	if err := x.Params(&p); err != nil {
		return err
	}
	if err := d.DB.DeleteProject(p.Name, p.RepoPath); err != nil {
		return fmt.Errorf("db: %w", err)
	}
	return nil
}
