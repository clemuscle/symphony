package handlers

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/metrics"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/clemuscle/symphony/internal/webhook"
	"github.com/go-chi/chi/v5"
)

// Types de tâches
const (
	KindCreateProject  = "create_project"
	KindDeleteProject  = "delete_project"
	KindDeploy         = "deploy"
	KindActionWebhook  = "action_webhook"
	KindActionPipeline = "action_pipeline"
)

// TaskDeps : ce dont les étapes des tâches ont besoin. Les providers sont
// résolus à chaque étape : la configuration peut changer pendant une tâche.
type TaskDeps struct {
	Providers *provider.Registry
	DB        *database.DB
	Templates *gitops.TemplateLoader
	Syncer    *gitops.Syncer
	Webhooks  *webhook.Holder
}

// startupTimeout : délai pour qu'un service déployé soit prêt (pull de
// l'image, synchronisation Argo CD, démarrage des pods)
var startupTimeout = 5 * time.Minute

// RegisterTasks déclare les types de tâches de Symphony
func RegisterTasks(r *tasks.Runner, d TaskDeps) {
	r.Register(tasks.Definition{Kind: KindCreateProject, Steps: []tasks.Step{
		{Label: "Repo et squelette", Run: d.createRepo},
		{Label: "Projet et webhook", Run: d.registerProject},
		{Label: "Première pipeline (image)", Run: d.waitFirstPipeline},
	}})
	r.Register(tasks.Definition{Kind: KindDeploy, Steps: []tasks.Step{
		{Label: "Déploiement", Run: d.deploy},
		{Label: "Démarrage du service", Run: d.waitRunning},
	}})
	r.Register(tasks.Definition{Kind: KindDeleteProject, Steps: []tasks.Step{
		{Label: "Déploiements", Run: d.deleteDeployments},
		{Label: "Repo et images", Run: d.deleteRepo},
		{Label: "Fiche catalogue", Run: d.deleteCatalogue},
		{Label: "Projet", Run: d.deleteRecord},
	}})
	r.Register(tasks.Definition{Kind: KindActionWebhook, Steps: []tasks.Step{
		{Label: "Appel du webhook", Run: d.callActionWebhook},
	}})
	r.Register(tasks.Definition{Kind: KindActionPipeline, Steps: []tasks.Step{
		{Label: "Pipeline", Run: d.runActionPipeline},
	}})

	// Fin de tâche : métriques, et audit des échecs (la demande elle-même est
	// journalisée à la soumission, avec l'adresse de l'appelant)
	r.OnFinish = func(t database.Task) {
		end := time.Now()
		if t.FinishedAt != nil {
			end = *t.FinishedAt
		}
		metrics.TaskFinished(t.Kind, t.Status, end.Sub(t.CreatedAt))
		if t.Status == database.TaskFailed {
			if err := d.DB.LogAudit(t.Kind+"_failed", t.Resource, map[string]any{
				"task_id": t.ID,
				"error":   t.Error,
			}, t.CreatedBy, ""); err != nil {
				slog.Warn("audit", "task", t.ID, "error", err)
			}
		}
	}
}

// submit soumet une tâche, journalise la demande et répond 202 avec la tâche
func submit(w http.ResponseWriter, r *http.Request, runner *tasks.Runner, db *database.DB,
	kind, resource string, params any, details map[string]any) {
	task, err := runner.Submit(kind, resource, params, userID(r))
	if err != nil {
		respondError(w, http.StatusInternalServerError, err.Error())
		return
	}
	if details == nil {
		details = map[string]any{}
	}
	details["task_id"] = task.ID
	audit(db, r, kind, resource, details)
	respond(w, http.StatusAccepted, task)
}

// busy répond 409 si une tâche est déjà en cours sur resource
func busy(w http.ResponseWriter, runner *tasks.Runner, resource string) bool {
	active, err := runner.Active(resource)
	if err != nil {
		respondError(w, http.StatusInternalServerError, "tâches: "+err.Error())
		return true
	}
	if active != nil {
		respondError(w, http.StatusConflict, fmt.Sprintf("%s : tâche #%d (%s) déjà en cours", resource, active.ID, active.Kind))
		return true
	}
	return false
}

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
			err = scm.CreateWebhook(repo.Path, s.Endpoint, s.Secret)
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

// ─── Supprimer un projet ─────────────────────────────────────────────────────

// deleteParams : ce qu'il faut pour finir la suppression même si le projet
// a déjà disparu de la base
type deleteParams struct {
	Name        string `json:"name"`
	RepoPath    string `json:"repo_path"`
	SCMProvider string `json:"scm_provider"`
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
	// Projets antérieurs à la migration 002 : pas de scm_provider, SCM par défaut
	scm, err := d.Providers.SCM(p.SCMProvider)
	if err != nil {
		return err
	}
	x.Progress("purge du registry puis suppression de %s (2 à 3 min sur GitLab)", p.RepoPath)
	if err := scm.DeleteRepo(p.RepoPath); err != nil && !errors.Is(err, provider.ErrNotFound) {
		return fmt.Errorf("suppression du repo: %w", err)
	}
	return nil
}

func (d TaskDeps) deleteCatalogue(_ context.Context, x *tasks.Exec) error {
	var p deleteParams
	if err := x.Params(&p); err != nil {
		return err
	}
	// La fiche est partie avec le repo
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

// ─── Actions Day-2 ──────────────────────────────────────────────────────────

type actionParams struct {
	Service    string            `json:"service"`
	Action     string            `json:"action"`
	Inputs     map[string]string `json:"inputs"`
	WebhookURL string            `json:"webhook_url,omitempty"`
	CIProvider string            `json:"ci_provider,omitempty"`
	Repo       string            `json:"repo,omitempty"`
	Vars       map[string]string `json:"vars,omitempty"`
}

func (d TaskDeps) callActionWebhook(_ context.Context, x *tasks.Exec) error {
	var p actionParams
	if err := x.Params(&p); err != nil {
		return err
	}
	status, err := callWebhook(p.WebhookURL, map[string]any{
		"service":      p.Service,
		"action":       p.Action,
		"inputs":       p.Inputs,
		"task_id":      x.Task().ID,
		"triggered_at": time.Now().UTC().Format(time.RFC3339),
	})
	x.Set("http_status", status)
	if err != nil {
		return fmt.Errorf("webhook: %w", err)
	}
	return nil
}

// runActionPipeline lance la pipeline de l'action, puis attend sa fin
func (d TaskDeps) runActionPipeline(_ context.Context, x *tasks.Exec) error {
	var p actionParams
	if err := x.Params(&p); err != nil {
		return err
	}
	ci, err := d.Providers.CI(p.CIProvider)
	if err != nil {
		return err
	}
	var pipeline provider.Pipeline
	if !x.Get("pipeline", &pipeline) {
		x.Progress("lancement sur %s", p.Repo)
		started, err := ci.TriggerPipeline(p.Repo, "main", p.Vars)
		if err != nil {
			return fmt.Errorf("pipeline: %w", err)
		}
		pipeline = *started
		x.Set("pipeline", pipeline)
		d.DB.CreatePipeline(&database.Pipeline{
			ProjectName: p.Repo,
			PipelineID:  pipeline.ID,
			CIProvider:  ci.ID(),
			Type:        "action",
			Status:      pipeline.Status,
			WebURL:      pipeline.WebURL,
			Ref:         pipeline.Ref,
			TriggeredBy: x.Task().CreatedBy,
		})
	}
	x.WaitPipeline(ci.ID(), p.Repo, pipeline.ID, pipeline.WebURL, time.Now())
	return nil
}

// ─── API ─────────────────────────────────────────────────────────────────────

// ListTasks : tâches récentes, ?resource= pour un projet ou un service
func ListTasks(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
		if limit <= 0 || limit > 200 {
			limit = 50
		}
		list, err := db.ListTasks(database.TaskFilter{Resource: r.URL.Query().Get("resource"), Limit: limit})
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respond(w, http.StatusOK, list)
	}
}

func GetTask(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		id, err := strconv.ParseInt(chi.URLParam(r, "id"), 10, 64)
		if err != nil {
			respondError(w, http.StatusBadRequest, "id invalide")
			return
		}
		t, err := db.GetTask(id)
		if tasks.IsNotFound(err) {
			respondError(w, http.StatusNotFound, err.Error())
			return
		}
		if err != nil {
			respondError(w, http.StatusInternalServerError, err.Error())
			return
		}
		respond(w, http.StatusOK, t)
	}
}

// TaskEvents diffuse les changements de tâches en Server-Sent Events
// (événement « task », la tâche en JSON). Un commentaire toutes les 20 s
// garde la connexion ouverte à travers les proxys.
func TaskEvents(hub *tasks.Hub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		flusher, ok := w.(http.Flusher)
		if !ok {
			respondError(w, http.StatusInternalServerError, "streaming non pris en charge")
			return
		}
		events, unsubscribe := hub.Subscribe()
		defer unsubscribe()

		w.Header().Set("Content-Type", "text/event-stream")
		w.Header().Set("Cache-Control", "no-cache")
		w.Header().Set("X-Accel-Buffering", "no") // nginx : pas de mise en tampon
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, "retry: 3000\n\n")
		flusher.Flush()

		ping := time.NewTicker(20 * time.Second)
		defer ping.Stop()
		for {
			select {
			case <-r.Context().Done():
				return
			case t, open := <-events:
				if !open {
					return // abonné trop lent : le navigateur se reconnecte et relit la liste
				}
				data, err := json.Marshal(t)
				if err != nil {
					continue
				}
				fmt.Fprintf(w, "event: task\ndata: %s\n\n", data)
				flusher.Flush()
			case <-ping.C:
				fmt.Fprint(w, ": ping\n\n")
				flusher.Flush()
			}
		}
	}
}
