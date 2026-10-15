package handlers

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/clemuscle/symphony/internal/catalogue"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/tasks"
	"github.com/go-chi/chi/v5"
)

func ListServices(store *catalogue.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		services := store.Search(
			q.Get("q"),
			q.Get("tier"),
			q.Get("lifecycle"),
		)
		if services == nil {
			services = []catalogue.Service{}
		}
		respond(w, http.StatusOK, services)
	}
}

func GetService(store *catalogue.Store) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name := chi.URLParam(r, "name")
		svc, ok := store.Get(name)
		if !ok {
			respondError(w, http.StatusNotFound, "service introuvable: "+name)
			return
		}
		respond(w, http.StatusOK, svc)
	}
}

var webhookClient = &http.Client{Timeout: 10 * time.Second}

func TriggerAction(store *catalogue.Store, providers *provider.Registry, db *database.DB, runner *tasks.Runner) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		svcName := chi.URLParam(r, "name")
		actionName := chi.URLParam(r, "action")

		svc, ok := store.Get(svcName)
		if !ok {
			respondError(w, http.StatusNotFound, "service introuvable: "+svcName)
			return
		}
		// La fiche porte le nom du projet
		if _, ok := authorizeProject(w, r, db, svcName); !ok {
			return
		}

		var action *catalogue.Action
		for i := range svc.Spec.Actions {
			if svc.Spec.Actions[i].Name == actionName {
				action = &svc.Spec.Actions[i]
				break
			}
		}
		if action == nil {
			respondError(w, http.StatusNotFound, "action introuvable: "+actionName)
			return
		}

		// Body vide autorisé (action sans inputs)
		raw := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil && !errors.Is(err, io.EOF) {
			respondError(w, http.StatusBadRequest, "body invalide")
			return
		}
		inputs, err := resolveInputs(action.Inputs, raw)
		if err != nil {
			respondError(w, http.StatusBadRequest, err.Error())
			return
		}

		// Une action à la fois par service ; elle s'exécute en tâche (202)
		resource := svcName
		if busy(w, r, runner, resource) {
			return
		}
		switch {
		case action.WebhookURL != "":
			submit(w, r, runner, db, KindActionWebhook, resource, actionParams{
				Service:    svcName,
				Action:     actionName,
				Inputs:     inputs,
				WebhookURL: action.WebhookURL,
			}, map[string]any{"action": actionName, "mode": "webhook", "inputs": inputs})

		case action.CIProvider != "":
			repoPath := repoPathFromSpec(svc.Spec.Repo)
			if repoPath == "" {
				respondError(w, http.StatusBadRequest, "spec.repo requis pour une action pipeline")
				return
			}
			ci, err := providers.CI(action.CIProvider)
			if err != nil {
				respondError(w, http.StatusBadRequest, "ci provider: "+err.Error())
				return
			}

			vars := make(map[string]string, len(inputs)+3)
			for k, v := range inputs {
				vars[k] = v
			}
			vars["SYMPHONY_SERVICE"] = svcName
			vars["SYMPHONY_ACTION"] = actionName
			if action.Pipeline != "" {
				vars["SYMPHONY_PIPELINE"] = action.Pipeline
			}
			submit(w, r, runner, db, KindActionPipeline, resource, actionParams{
				Service:    svcName,
				Action:     actionName,
				Inputs:     inputs,
				CIProvider: ci.ID(),
				Repo:       repoPath,
				Vars:       vars,
			}, map[string]any{"action": actionName, "mode": "pipeline", "ci_provider": ci.ID(), "repo": repoPath, "inputs": inputs})

		default:
			respondError(w, http.StatusBadRequest, "action non configurée : définir webhook_url ou ci_provider")
		}
	}
}

// resolveInputs valide les valeurs reçues contre les inputs déclarés par l'action :
// défauts, champs requis, entiers et bornes, options. Les clés non déclarées sont
// refusées : en mode pipeline, elles deviendraient des variables CI arbitraires
// (et pourraient écraser CI_REGISTRY_IMAGE, etc.).
func resolveInputs(declared []catalogue.Input, raw map[string]any) (map[string]string, error) {
	known := make(map[string]bool, len(declared))
	for _, in := range declared {
		known[in.ID] = true
	}
	for k := range raw {
		if !known[k] {
			return nil, fmt.Errorf("input inconnu: %s", k)
		}
	}

	out := make(map[string]string, len(declared))
	for _, in := range declared {
		val := ""
		if v, ok := raw[in.ID]; ok && v != nil {
			val = strings.TrimSpace(fmt.Sprint(v))
		}
		if val == "" {
			val = in.Default
		}
		if val == "" {
			if in.Required {
				return nil, fmt.Errorf("input requis: %s", in.ID)
			}
			continue
		}

		if in.Type == "integer" {
			n, err := strconv.Atoi(val)
			if err != nil {
				return nil, fmt.Errorf("input %s: entier attendu, reçu %q", in.ID, val)
			}
			if in.Min != nil && n < *in.Min {
				return nil, fmt.Errorf("input %s: minimum %d", in.ID, *in.Min)
			}
			if in.Max != nil && n > *in.Max {
				return nil, fmt.Errorf("input %s: maximum %d", in.ID, *in.Max)
			}
		}
		if len(in.Options) > 0 && !slices.Contains(in.Options, val) {
			return nil, fmt.Errorf("input %s: valeur %q hors options %v", in.ID, val, in.Options)
		}
		out[in.ID] = val
	}
	return out, nil
}

// callWebhook envoie le payload en POST et renvoie le code HTTP ; erreur si >= 400
func callWebhook(url string, payload map[string]any) (int, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return 0, err
	}
	resp, err := webhookClient.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
		return resp.StatusCode, fmt.Errorf("status %d — %s", resp.StatusCode, strings.TrimSpace(string(msg)))
	}
	return resp.StatusCode, nil
}

// repoPathFromSpec accepte "groupe/projet" ou une URL complète (http(s)://host/groupe/projet[.git])
func repoPathFromSpec(repo string) string {
	repo = strings.TrimSpace(repo)
	if u, err := url.Parse(repo); err == nil && u.Host != "" {
		repo = u.Path
	}
	return strings.TrimSuffix(strings.Trim(repo, "/"), ".git")
}
