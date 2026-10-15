package handlers

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
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
// chacun (celui de son golden path s'il en change, sinon celui de la
// configuration)
func projectEnvironments(envs *environments.Holder, templates *gitops.TemplateLoader, p *database.Project) []environments.Environment {
	list := envs.List()
	out := make([]environments.Environment, len(list))
	gp, gpErr := templates.GetGoldenPath(p.GoldenPath)
	for i, e := range list {
		if gpErr == nil {
			e.Runtime = gp.RuntimeFor(e.Name, e.Runtime)
		}
		out[i] = e
	}
	return out
}

// environmentRuntime : le runtime du projet dans l'environnement env
func environmentRuntime(envs *environments.Holder, templates *gitops.TemplateLoader, p *database.Project, env string) (string, bool) {
	for _, e := range projectEnvironments(envs, templates, p) {
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
func Promote(providers *provider.Registry, db *database.DB, templates *gitops.TemplateLoader,
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
		fromRT, _ := environmentRuntime(envs, templates, proj, from.Name)
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
		toRT, _ := environmentRuntime(envs, templates, proj, to.Name)
		if busy(w, r, runner, proj.Name) {
			return
		}
		submit(w, r, runner, db, KindDeploy, proj.Name, deployParams{
			ProjectName:  proj.Name,
			Environment:  to.Name,
			Image:        image,
			Tag:          tag,
			Port:         proj.Port,
			Runtime:      toRT,
			PromotedFrom: from.Name,
		}, map[string]any{
			"promotion": from.Name + " → " + to.Name,
			"image":     image + ":" + tag,
			"provider":  toRT,
		})
	}
}
