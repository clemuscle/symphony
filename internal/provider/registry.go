package provider

import (
	"fmt"
	"sort"
	"sync"
)

// Registry contient les providers par id. Son contenu peut être remplacé d'un
// bloc (Replace) au rechargement de la configuration : les handlers gardent le
// même *Registry et voient soit l'ancien jeu de providers, soit le nouveau.
type Registry struct {
	mu  sync.RWMutex
	set *providerSet
}

type providerSet struct {
	scm      map[string]SCMProvider
	ci       map[string]CIProvider
	registry map[string]RegistryProvider
	runtime  map[string]RuntimeProvider
	secrets  map[string]SecretsProvider
	identity map[string]IdentityProvider
	notify   map[string]NotificationProvider

	defaultSCM      string
	defaultCI       string
	defaultRegistry string
	defaultRuntime  string
	defaultSecrets  string
	defaultIdentity string
}

func NewRegistry() *Registry {
	return &Registry{set: &providerSet{
		scm:      make(map[string]SCMProvider),
		ci:       make(map[string]CIProvider),
		registry: make(map[string]RegistryProvider),
		runtime:  make(map[string]RuntimeProvider),
		secrets:  make(map[string]SecretsProvider),
		identity: make(map[string]IdentityProvider),
		notify:   make(map[string]NotificationProvider),
	}}
}

// Replace remplace tous les providers par ceux de next, qui ne doit plus être
// modifié ensuite
func (r *Registry) Replace(next *Registry) {
	next.mu.RLock()
	set := next.set
	next.mu.RUnlock()

	r.mu.Lock()
	r.set = set
	r.mu.Unlock()
}

func (r *Registry) RegisterSCM(p SCMProvider, isDefault bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set.scm[p.ID()] = p
	if isDefault || len(r.set.scm) == 1 {
		r.set.defaultSCM = p.ID()
	}
}

func (r *Registry) RegisterCI(p CIProvider, isDefault bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set.ci[p.ID()] = p
	if isDefault || len(r.set.ci) == 1 {
		r.set.defaultCI = p.ID()
	}
}

func (r *Registry) RegisterRegistry(p RegistryProvider, isDefault bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set.registry[p.ID()] = p
	if isDefault || len(r.set.registry) == 1 {
		r.set.defaultRegistry = p.ID()
	}
}

func (r *Registry) RegisterRuntime(p RuntimeProvider, isDefault bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set.runtime[p.ID()] = p
	if isDefault || len(r.set.runtime) == 1 {
		r.set.defaultRuntime = p.ID()
	}
}

func (r *Registry) RegisterSecrets(p SecretsProvider, isDefault bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set.secrets[p.ID()] = p
	if isDefault || len(r.set.secrets) == 1 {
		r.set.defaultSecrets = p.ID()
	}
}

func (r *Registry) RegisterIdentity(p IdentityProvider, isDefault bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set.identity[p.ID()] = p
	if isDefault || len(r.set.identity) == 1 {
		r.set.defaultIdentity = p.ID()
	}
}

// RegisterNotifications : pas de canal par défaut, chaque équipe désigne les
// siens (section notifications de la configuration)
func (r *Registry) RegisterNotifications(p NotificationProvider) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.set.notify[p.ID()] = p
}

func (r *Registry) current() *providerSet {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.set
}

// lookup renvoie le provider id, ou le provider par défaut si id est vide
func lookup[P any](kind string, m map[string]P, id, defaultID string) (P, error) {
	if id == "" {
		id = defaultID
	}
	p, ok := m[id]
	if !ok {
		var zero P
		return zero, fmt.Errorf("%s provider %q not found", kind, id)
	}
	return p, nil
}

func (r *Registry) SCM(id string) (SCMProvider, error) {
	s := r.current()
	return lookup("scm", s.scm, id, s.defaultSCM)
}

func (r *Registry) CI(id string) (CIProvider, error) {
	s := r.current()
	return lookup("ci", s.ci, id, s.defaultCI)
}

func (r *Registry) GetRegistry(id string) (RegistryProvider, error) {
	s := r.current()
	return lookup("registry", s.registry, id, s.defaultRegistry)
}

func (r *Registry) Runtime(id string) (RuntimeProvider, error) {
	s := r.current()
	return lookup("runtime", s.runtime, id, s.defaultRuntime)
}

func (r *Registry) Secrets(id string) (SecretsProvider, error) {
	s := r.current()
	return lookup("secrets", s.secrets, id, s.defaultSecrets)
}

func (r *Registry) Identity(id string) (IdentityProvider, error) {
	s := r.current()
	return lookup("identity", s.identity, id, s.defaultIdentity)
}

func (r *Registry) Notifications(id string) (NotificationProvider, error) {
	s := r.current()
	return lookup("notifications", s.notify, id, "")
}

func sortedIDs[P any](m map[string]P) []string {
	ids := make([]string, 0, len(m))
	for id := range m {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (r *Registry) ListSCM() []string        { return sortedIDs(r.current().scm) }
func (r *Registry) ListCI() []string         { return sortedIDs(r.current().ci) }
func (r *Registry) ListRegistries() []string { return sortedIDs(r.current().registry) }
func (r *Registry) ListRuntimes() []string   { return sortedIDs(r.current().runtime) }
func (r *Registry) ListSecrets() []string    { return sortedIDs(r.current().secrets) }
func (r *Registry) ListIdentity() []string   { return sortedIDs(r.current().identity) }
func (r *Registry) ListNotifications() []string {
	return sortedIDs(r.current().notify)
}

// Entry : un provider en place, avec sa section (scm, ci, registry, runtime,
// secrets, identity, notifications) et sa vérification
type Entry struct {
	Kind    string
	ID      string
	Default bool
	Check   func() (string, error)
}

// Entries : tous les providers en place, par section puis par id
func (r *Registry) Entries() []Entry {
	s := r.current()
	var out []Entry
	add := func(kind string, ids []string, def string, check func(string) func() (string, error)) {
		for _, id := range ids {
			out = append(out, Entry{Kind: kind, ID: id, Default: id == def, Check: check(id)})
		}
	}
	add("scm", sortedIDs(s.scm), s.defaultSCM, func(id string) func() (string, error) { return s.scm[id].Check })
	add("ci", sortedIDs(s.ci), s.defaultCI, func(id string) func() (string, error) { return s.ci[id].Check })
	add("registry", sortedIDs(s.registry), s.defaultRegistry, func(id string) func() (string, error) { return s.registry[id].Check })
	add("runtime", sortedIDs(s.runtime), s.defaultRuntime, func(id string) func() (string, error) { return s.runtime[id].Check })
	add("secrets", sortedIDs(s.secrets), s.defaultSecrets, func(id string) func() (string, error) { return s.secrets[id].Check })
	add("identity", sortedIDs(s.identity), s.defaultIdentity, func(id string) func() (string, error) { return s.identity[id].Check })
	add("notifications", sortedIDs(s.notify), "", func(id string) func() (string, error) { return s.notify[id].Check })
	return out
}
