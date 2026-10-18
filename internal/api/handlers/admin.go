package handlers

import (
	"errors"
	"net/http"
	"sync"
	"time"

	"github.com/clemuscle/symphony/internal/auth"
	"github.com/clemuscle/symphony/internal/config"
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
	"github.com/clemuscle/symphony/internal/provider/instrumented"
)

func ListStacks(stacks *gitops.StackLoader) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		list := stacks.GetStacks()
		if list == nil {
			list = []gitops.Stack{}
		}
		respond(w, http.StatusOK, list)
	}
}

func ListProviders(providers *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		respond(w, http.StatusOK, map[string]any{
			"scm":      providers.ListSCM(),
			"ci":       providers.ListCI(),
			"registry": providers.ListRegistries(),
			"runtime":  providers.ListRuntimes(),
			"secrets":  providers.ListSecrets(),
			"identity": providers.ListIdentity(),
		})
	}
}

// ListIdentityGroups : groupes de l'annuaire du provider identity par défaut
// (équipes proposées comme owner d'un projet). supported vaut false si le
// provider n'a pas d'annuaire, ou s'il n'y a pas de provider identity.
// Les groupes qui donnent un rôle (auth.roles) ne sont pas des équipes : ils
// sont retirés.
func ListIdentityGroups(providers *provider.Registry, security *auth.Holder) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		idp, err := providers.Identity(r.URL.Query().Get("provider"))
		if err != nil {
			respond(w, http.StatusOK, map[string]any{"supported": false, "groups": []provider.Group{}})
			return
		}
		groups, err := idp.ListGroups()
		if errors.Is(err, provider.ErrUnsupported) {
			respond(w, http.StatusOK, map[string]any{"provider": idp.ID(), "supported": false, "groups": []provider.Group{}})
			return
		}
		if err != nil {
			providerError(w, r, "annuaire "+idp.ID(), err)
			return
		}
		teams := make([]provider.Group, 0, len(groups))
		for _, g := range groups {
			if !security.IsRoleGroup(g.Name) {
				teams = append(teams, g)
			}
		}
		respond(w, http.StatusOK, map[string]any{"provider": idp.ID(), "supported": true, "groups": teams})
	}
}

// ForceSync recharge le catalogue et les stacks depuis Git, et relance la
// découverte des services existants
func ForceSync(syncer *gitops.Syncer, discovery *gitops.Discoverer, stacks *gitops.StackLoader, db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		audit(db, r, "force_sync", "catalogue+stacks+discovery", nil)
		go syncer.ForceSync()
		go stacks.Reload()
		discovery.Wake()
		respond(w, http.StatusOK, map[string]string{"status": "sync déclenché"})
	}
}

func ListAudit(db *database.DB) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries, err := db.ListAudit(50)
		if err != nil {
			internalError(w, r, err)
			return
		}
		if entries == nil {
			entries = []database.AuditEntry{}
		}
		respond(w, http.StatusOK, entries)
	}
}

// AdminStatus : ce que l'admin plateforme doit savoir sans lire les logs :
// configuration en place et dernier refus, providers et leur dernier appel,
// stacks chargées et refusées, fiches catalogue en erreur, dernier scan de la
// découverte
func AdminStatus(providers *provider.Registry, configStatus func() config.Status, stacks *gitops.StackLoader, syncer *gitops.Syncer, discovery *gitops.Discoverer) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := configStatus()
		entries := providers.Entries()
		list := make([]providerStatus, 0, len(entries))
		for _, e := range entries {
			ps := providerStatus{Kind: e.Kind, ID: e.ID, Type: cfg.Types[e.Kind+"/"+e.ID], Default: e.Default}
			if c, ok := instrumented.Last(e.Kind, e.ID); ok {
				ps.LastCall = &c
			}
			list = append(list, ps)
		}
		loaded := []string{}
		for _, s := range stacks.GetStacks() {
			loaded = append(loaded, s.ID)
		}
		respond(w, http.StatusOK, map[string]any{
			"config":    cfg,
			"providers": list,
			"stacks":    stacksStatus{StackReport: stacks.Report(), Loaded: loaded},
			"catalogue": syncer.Report(),
			"discovery": discovery.Report(),
		})
	}
}

type providerStatus struct {
	Kind     string                 `json:"kind"`
	ID       string                 `json:"id"`
	Type     string                 `json:"type"`
	Default  bool                   `json:"default"`
	LastCall *instrumented.LastCall `json:"last_call"`
}

type stacksStatus struct {
	gitops.StackReport
	Loaded []string `json:"loaded"`
}

// providerCheck : résultat de la vérification d'un provider
type providerCheck struct {
	Kind       string `json:"kind"`
	ID         string `json:"id"`
	OK         bool   `json:"ok"`
	Detail     string `json:"detail,omitempty"`
	Error      string `json:"error,omitempty"`
	DurationMS int64  `json:"duration_ms"`
}

// checkTimeout : au-delà, un provider qui ne répond pas est signalé sans
// attendre la fin de son appel
var checkTimeout = 20 * time.Second

// CheckProviders vérifie chaque provider en place, en parallèle : il répond
// et accepte ses identifiants. Rien n'est modifié chez les providers.
func CheckProviders(providers *provider.Registry) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		entries := providers.Entries()
		results := make([]providerCheck, len(entries))
		var wg sync.WaitGroup
		for i, e := range entries {
			wg.Add(1)
			go func() {
				defer wg.Done()
				results[i] = check(e)
			}()
		}
		wg.Wait()
		respond(w, http.StatusOK, results)
	}
}

func check(e provider.Entry) providerCheck {
	res := providerCheck{Kind: e.Kind, ID: e.ID}
	type outcome struct {
		detail string
		err    error
	}
	done := make(chan outcome, 1)
	start := time.Now()
	go func() {
		detail, err := e.Check()
		done <- outcome{detail, err}
	}()
	select {
	case o := <-done:
		res.OK, res.Detail = o.err == nil, o.detail
		if o.err != nil {
			res.Error = o.err.Error()
		}
	case <-time.After(checkTimeout):
		res.Error = "pas de réponse après " + checkTimeout.String()
	}
	res.DurationMS = time.Since(start).Milliseconds()
	return res
}
