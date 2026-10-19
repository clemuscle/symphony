package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/environments"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
)

// ListEnvironments : les environnements de la configuration, dans l'ordre de
// promotion
func ListEnvironments(envs *environments.Holder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, envs.List())
	}
}

// projectEnvironments : les environnements avec le runtime du projet dans
// chacun (celui de sa stack si elle en change, sinon celui de la
// configuration)
func projectEnvironments(envs *environments.Holder, stacks *gitops.StackLoader, p *database.Project) []environments.Environment {
	list := envs.List()
	out := make([]environments.Environment, len(list))
	gp, gpErr := stacks.GetStack(p.Stack)
	for i, e := range list {
		if gpErr == nil {
			e.Runtime = gp.RuntimeFor(e.Name, e.Runtime)
		}
		out[i] = e
	}
	return out
}

// environmentRuntime : le runtime du projet dans l'environnement env
func environmentRuntime(envs *environments.Holder, stacks *gitops.StackLoader, p *database.Project, env string) (string, bool) {
	for _, e := range projectEnvironments(envs, stacks, p) {
		if e.Name == env {
			return e.Runtime, true
		}
	}
	return "", false
}

// appEnv : variables de l'application dans l'environnement env, déclarées
// dans la fiche du projet (spec.environments[].env). Les valeurs secret://
// sont séparées des valeurs en clair et vérifiées : un projet ne lit que ses
// propres secrets (apps/<projet>/…), jamais ceux de Symphony.
func appEnv(store *catalogue.Store, project, env string) (plain map[string]string, secrets map[string]provider.SecretRef, err error) {
	plain, secrets = map[string]string{}, map[string]provider.SecretRef{}
	svc, ok := store.Get(project)
	if !ok {
		return plain, secrets, nil
	}
	for _, e := range svc.Spec.Environments {
		if e.Name != env {
			continue
		}
		for k, v := range e.Env {
			if !provider.IsSecretRef(v) {
				plain[k] = v
				continue
			}
			ref, err := provider.ParseSecretRef(v)
			if err != nil {
				return nil, nil, fmt.Errorf("variable %s : %w", k, err)
			}
			if err := checkAppSecret(project, ref); err != nil {
				return nil, nil, fmt.Errorf("variable %s : %w", k, err)
			}
			secrets[k] = ref
		}
	}
	return plain, secrets, nil
}

// checkAppSecret : une application ne lit que les secrets de son projet,
// sous apps/<projet>/, et jamais l'environnement de Symphony (provider env)
func checkAppSecret(project string, ref provider.SecretRef) error {
	if ref.Provider == "env" {
		return fmt.Errorf("%s : les variables d'environnement de Symphony ne sont pas accessibles aux applications", ref)
	}
	if !strings.HasPrefix(ref.Path, "apps/"+project+"/") {
		return fmt.Errorf("%s : un projet ne lit que ses secrets, sous apps/%s/", ref, project)
	}
	return nil
}

// Promote déploie dans l'environnement to l'image qui tourne, saine, dans
// l'environnement précédent : seul moyen d'atteindre un environnement protégé
func Promote(providers *provider.Registry, db *database.DB, stacks *gitops.StackLoader,
	envs *environments.Holder, runner *tasks.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		var req struct {
			To string `json:"to"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "body invalide")
			return
		}
		proj, ok := authorizeProject(w, r, db, name)
		if !ok {
			return
		}
		if proj == nil {
			respondError(w, http.StatusNotFound, "projet introuvable : "+name)
			return
		}
		to, _, found := envs.Find(req.To)
		if !found {
			respondError(w, http.StatusBadRequest, "environnement inconnu : "+req.To)
			return
		}
		from, ok := envs.Previous(to.Name)
		if !ok {
			respondError(w, http.StatusBadRequest, to.Name+" est le premier environnement : rien à promouvoir")
			return
		}

		// L'image qui tourne dans l'environnement précédent, saine
		fromRT, _ := environmentRuntime(envs, stacks, proj, from.Name)
		rt, err := providers.Runtime(fromRT)
		if err != nil {
			respondError(w, http.StatusBadRequest, "runtime de "+from.Name+" : "+err.Error())
			return
		}
		current, err := rt.Status(provider.DeploymentName(proj.Name, from.Name))
		if errors.Is(err, provider.ErrNotFound) {
			respondError(w, http.StatusConflict, fmt.Sprintf("%s n'est pas déployé en %s : rien à promouvoir", proj.Name, from.Name))
			return
		}
		if err != nil {
			providerError(w, r, "état du déploiement en "+from.Name, err)
			return
		}
		if current.Status != "running" || current.Image == "" {
			respondError(w, http.StatusConflict, fmt.Sprintf("le déploiement en %s n'est pas sain (%s) : promotion refusée", from.Name, current.Status))
			return
		}
		image, tag := provider.SplitImage(current.Image, "")
		// Un tag mobile (latest) ne désigne pas une version : la production
		// pourrait changer sans nouvelle promotion
		if tag == "" || tag == "latest" {
			respondError(w, http.StatusConflict, fmt.Sprintf("%s tourne en %s : déployer un tag immuable (sha) en %s pour pouvoir le promouvoir",
				from.Name, current.Image, from.Name))
			return
		}
		toRT, _ := environmentRuntime(envs, stacks, proj, to.Name)
		if busy(w, r, runner, proj.Name) {
			return
		}
		approval := needsApproval(r, to, proj)
		submit(w, r, runner, db, KindDeploy, proj.Name, deployParams{
			ProjectName:  proj.Name,
			Environment:  to.Name,
			Image:        image,
			Tag:          tag,
			Port:         proj.Port,
			Runtime:      toRT,
			Origin:       database.OriginPromotion,
			PromotedFrom: from.Name,
			Approval:     approval,
		}, map[string]any{
			"promotion": from.Name + " → " + to.Name,
			"image":     image + ":" + tag,
			"provider":  toRT,
			"approval":  approval,
		})
	}
}

// ─── Historique et retour arrière ────────────────────────────────────────────

// ListReleases : l'historique des déploiements (?project=, ?environment=,
// ?latest=true pour la dernière version de chaque projet dans chaque
// environnement : la matrice versions × environnements)
func ListReleases(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		limit, _ := strconv.Atoi(q.Get("limit"))
		if limit <= 0 || limit > 500 {
			limit = 50
		}
		f := database.ReleaseFilter{Project: q.Get("project"), Latest: q.Get("latest") == "true", Limit: limit}
		if q.Has("environment") {
			env := q.Get("environment")
			f.Environment = &env
		}
		list, err := db.ListReleases(f)
		if err != nil {
			internalError(w, r, err)
			return
		}
		respond(w, http.StatusOK, list)
	}
}

// ranOnce : la version a démarré dans son environnement (elle y a tourné,
// même si elle a été arrêtée depuis)
func ranOnce(d database.Deployment) bool {
	return d.Status == database.ReleaseRunning || d.Status == database.ReleaseStopped
}

// Rollback redéploie dans un environnement une version qui y a déjà tourné :
// release_id, sinon la plus récente dont l'image diffère de celle en place.
// Un environnement protégé l'accepte (la version y est déjà passée) ;
// l'approbation, elle, s'applique comme à un déploiement.
func Rollback(providers *provider.Registry, db *database.DB, stacks *gitops.StackLoader,
	envs *environments.Holder, runner *tasks.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		var req struct {
			Environment string `json:"environment"`
			ReleaseID   int    `json:"release_id"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			respondError(w, http.StatusBadRequest, "body invalide")
			return
		}
		proj, ok := authorizeProject(w, r, db, name)
		if !ok {
			return
		}
		if proj == nil {
			respondError(w, http.StatusNotFound, "projet introuvable : "+name)
			return
		}
		env := environments.Environment{}
		rtID := proj.RTProvider
		if len(envs.List()) > 0 {
			e, _, found := envs.Find(req.Environment)
			if !found {
				respondError(w, http.StatusBadRequest, "environment requis : "+envNames(envs))
				return
			}
			env = e
			rtID, _ = environmentRuntime(envs, stacks, proj, env.Name)
		}
		rt, err := providers.Runtime(rtID)
		if err != nil {
			respondError(w, http.StatusBadRequest, "runtime : "+err.Error())
			return
		}
		history, err := db.ListReleases(database.ReleaseFilter{Project: proj.Name, Environment: &env.Name, Limit: 200})
		if err != nil {
			internalError(w, r, err)
			return
		}

		// L'image en place : celle du runtime, sinon la dernière de l'historique
		current := ""
		if st, err := rt.Status(provider.DeploymentName(proj.Name, env.Name)); err == nil {
			current = st.Image
		} else if !errors.Is(err, provider.ErrNotFound) {
			providerError(w, r, "état du déploiement", err)
			return
		}
		if current == "" && len(history) > 0 {
			current = history[0].Image + ":" + orLatest(history[0].Tag)
		}

		var target *database.Deployment
		for i := range history {
			h := &history[i]
			if req.ReleaseID != 0 {
				if h.ID == req.ReleaseID {
					target = h
					break
				}
				continue
			}
			if ranOnce(*h) && h.Tag != "" && h.Tag != "latest" && h.Image+":"+h.Tag != current {
				target = h
				break
			}
		}
		switch {
		case target == nil && req.ReleaseID != 0:
			respondError(w, http.StatusNotFound, fmt.Sprintf("version #%d absente de l'historique de %s%s", req.ReleaseID, proj.Name, inEnv(env.Name)))
			return
		case target == nil:
			respondError(w, http.StatusConflict, fmt.Sprintf("aucune version précédente de %s%s à laquelle revenir", proj.Name, inEnv(env.Name)))
			return
		case !ranOnce(*target):
			respondError(w, http.StatusConflict, fmt.Sprintf("la version #%d n'a jamais démarré (%s) : retour refusé", target.ID, target.Status))
			return
		case target.Tag == "" || target.Tag == "latest":
			respondError(w, http.StatusConflict, fmt.Sprintf("la version #%d est un tag mobile (%s) : elle ne désigne pas une version", target.ID, orLatest(target.Tag)))
			return
		}
		if busy(w, r, runner, proj.Name) {
			return
		}
		approval := needsApproval(r, env, proj)
		submit(w, r, runner, db, KindDeploy, proj.Name, deployParams{
			ProjectName: proj.Name,
			Environment: env.Name,
			Image:       target.Image,
			Tag:         target.Tag,
			Port:        proj.Port,
			Runtime:     rt.ID(),
			Origin:      database.OriginRollback,
			Approval:    approval,
		}, map[string]any{
			"rollback":    current + " → " + target.Image + ":" + target.Tag,
			"release_id":  target.ID,
			"environment": env.Name,
			"provider":    rt.ID(),
			"approval":    approval,
		})
	}
}

func inEnv(env string) string {
	if env == "" {
		return ""
	}
	return " en " + env
}
