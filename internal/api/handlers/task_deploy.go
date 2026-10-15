package handlers

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
)

// ─── Déployer ────────────────────────────────────────────────────────────────

type deployParams struct {
	ProjectName string            `json:"project_name"`
	Image       string            `json:"image"`
	Tag         string            `json:"tag"`
	Port        int               `json:"port"`
	HostPort    int               `json:"host_port"`
	Runtime     string            `json:"runtime"`
	EnvVars     map[string]string `json:"env_vars"`
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
	x.Progress("%s : %s:%s", rt.ID(), p.Image, orLatest(p.Tag))
	deployment, err := rt.Deploy(provider.DeployRequest{
		Name:     p.ProjectName,
		Image:    p.Image,
		Tag:      p.Tag,
		Port:     p.Port,
		HostPort: p.HostPort,
		EnvVars:  p.EnvVars,
		// Identifiants lus au moment du déploiement, jamais enregistrés dans la tâche
		RegistryAuth: registryAuth(d.Providers, d.DB, p.ProjectName, p.Image),
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
