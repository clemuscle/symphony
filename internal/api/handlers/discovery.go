package handlers

import (
	"github.com/clemuscle/symphony/internal/database"
	"github.com/clemuscle/symphony/internal/gitops"
	"github.com/clemuscle/symphony/internal/provider"
)

// DiscoveryStore : les projets en base, vus par la découverte
type DiscoveryStore struct {
	DB        *database.DB
	Providers *provider.Registry
}

func (s DiscoveryStore) KnownProjects() ([]gitops.KnownProject, error) {
	projects, err := s.DB.ListProjects()
	if err != nil {
		return nil, err
	}
	known := make([]gitops.KnownProject, len(projects))
	for i, p := range projects {
		scm := p.SCMProvider
		if scm == "" { // projets antérieurs à la migration 002 : SCM par défaut
			if def, err := s.Providers.SCM(""); err == nil {
				scm = def.ID()
			}
		}
		known[i] = gitops.KnownProject{Name: p.Name, RepoPath: p.RepoPath, SCMProvider: scm, DiscoveredFrom: p.DiscoveredFrom}
	}
	return known, nil
}

func (s DiscoveryStore) project(f gitops.FoundProject) *database.Project {
	p := &database.Project{
		Name:           f.Name,
		Description:    f.Description,
		Language:       f.Language,
		Type:           f.Type,
		Port:           f.Port,
		SCMProvider:    f.Source.SCM,
		CIProvider:     f.Source.CI,
		RegProvider:    f.Source.Registry,
		RepoPath:       f.Repo.Path,
		RepoURL:        f.Repo.HTTPCloneURL,
		Team:           f.Team,
		CreatedBy:      "découverte",
		Branch:         f.Repo.DefaultBranch,
		DiscoveredFrom: f.Source.Key(),
	}
	if f.Source.Registry != "" {
		if reg, err := s.Providers.GetRegistry(f.Source.Registry); err == nil {
			p.RegistryURL = reg.GetImageURL(f.Repo.Path)
		}
	}
	return p
}

func (s DiscoveryStore) AddProject(f gitops.FoundProject) error {
	return s.DB.CreateProject(s.project(f))
}

func (s DiscoveryStore) UpdateProject(f gitops.FoundProject) error {
	return s.DB.UpdateDiscoveredProject(s.project(f))
}
