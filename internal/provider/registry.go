package provider

import "fmt"

type Registry struct {
	scm      map[string]SCMProvider
	ci       map[string]CIProvider
	registry map[string]RegistryProvider
	runtime  map[string]RuntimeProvider

	defaultSCM      string
	defaultCI       string
	defaultRegistry string
	defaultRuntime  string
}

func NewRegistry() *Registry {
	return &Registry{
		scm:      make(map[string]SCMProvider),
		ci:       make(map[string]CIProvider),
		registry: make(map[string]RegistryProvider),
		runtime:  make(map[string]RuntimeProvider),
	}
}

func (r *Registry) RegisterSCM(p SCMProvider, isDefault bool) {
	r.scm[p.ID()] = p
	if isDefault || len(r.scm) == 1 {
		r.defaultSCM = p.ID()
	}
}

func (r *Registry) RegisterCI(p CIProvider, isDefault bool) {
	r.ci[p.ID()] = p
	if isDefault || len(r.ci) == 1 {
		r.defaultCI = p.ID()
	}
}

func (r *Registry) RegisterRegistry(p RegistryProvider, isDefault bool) {
	r.registry[p.ID()] = p
	if isDefault || len(r.registry) == 1 {
		r.defaultRegistry = p.ID()
	}
}

func (r *Registry) RegisterRuntime(p RuntimeProvider, isDefault bool) {
	r.runtime[p.ID()] = p
	if isDefault || len(r.runtime) == 1 {
		r.defaultRuntime = p.ID()
	}
}

func (r *Registry) SCM(id string) (SCMProvider, error) {
	if id == "" {
		id = r.defaultSCM
	}
	p, ok := r.scm[id]
	if !ok {
		return nil, fmt.Errorf("scm provider %q not found", id)
	}
	return p, nil
}

func (r *Registry) CI(id string) (CIProvider, error) {
	if id == "" {
		id = r.defaultCI
	}
	p, ok := r.ci[id]
	if !ok {
		return nil, fmt.Errorf("ci provider %q not found", id)
	}
	return p, nil
}

func (r *Registry) GetRegistry(id string) (RegistryProvider, error) {
	if id == "" {
		id = r.defaultRegistry
	}
	p, ok := r.registry[id]
	if !ok {
		return nil, fmt.Errorf("registry provider %q not found", id)
	}
	return p, nil
}

func (r *Registry) Runtime(id string) (RuntimeProvider, error) {
	if id == "" {
		id = r.defaultRuntime
	}
	p, ok := r.runtime[id]
	if !ok {
		return nil, fmt.Errorf("runtime provider %q not found", id)
	}
	return p, nil
}

func (r *Registry) ListSCM() []string {
	ids := make([]string, 0, len(r.scm))
	for id := range r.scm {
		ids = append(ids, id)
	}
	return ids
}

func (r *Registry) ListCI() []string {
	ids := make([]string, 0, len(r.ci))
	for id := range r.ci {
		ids = append(ids, id)
	}
	return ids
}

func (r *Registry) ListRegistries() []string {
	ids := make([]string, 0, len(r.registry))
	for id := range r.registry {
		ids = append(ids, id)
	}
	return ids
}

func (r *Registry) ListRuntimes() []string {
	ids := make([]string, 0, len(r.runtime))
	for id := range r.runtime {
		ids = append(ids, id)
	}
	return ids
}
