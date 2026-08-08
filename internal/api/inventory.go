package api

import (
	"log"
	"net/http"

	"github.com/yourorg/symphony/internal/database"
	"github.com/yourorg/symphony/internal/providers"
)

type inventorySummary struct {
	Projects   int `json:"projects"`
	Containers int `json:"containers_running"`
	Recettes   int `json:"recettes_active"`
	Pipelines  int `json:"pipelines_running"`
}

type inventoryResponse struct {
	Summary    inventorySummary          `json:"summary"`
	Projects   []database.Project        `json:"projects"`
	Containers []database.Deployment     `json:"containers"`
	Recettes   []database.Deployment     `json:"recettes"`
	Pipelines  []database.Pipeline       `json:"pipelines"`
	Live       []providers.ContainerInfo `json:"live_containers,omitempty"`
}

func (s *Server) getInventory(w http.ResponseWriter, r *http.Request) {
	projects, err := s.listProjectsScoped(r)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	groups, unrestricted := s.scopeGroups(r)

	var containers []database.Deployment
	if unrestricted {
		containers, err = s.db.ListAllActiveDeployments()
	} else {
		containers, err = s.db.ListAllActiveDeploymentsForGroups(groups)
	}
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var recettes []database.Deployment
	if unrestricted {
		recettes, err = s.db.ListAllActiveRecettes()
	} else {
		recettes, err = s.db.ListAllActiveRecettesForGroups(groups)
	}
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	var pipelines []database.Pipeline
	if unrestricted {
		pipelines, err = s.db.ListActivePipelines()
	} else {
		pipelines, err = s.db.ListActivePipelinesForGroups(groups)
	}
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Containers live Docker — best-effort, non bloquant si le daemon est absent.
	var live []providers.ContainerInfo
	if pvds := s.getProviders(); pvds != nil {
		if l, err := pvds.Deploy.ListContainers(); err != nil {
			log.Printf("inventory: docker list containers: %v", err)
		} else {
			live = l
		}
	}

	if projects == nil {
		projects = []database.Project{}
	}
	if containers == nil {
		containers = []database.Deployment{}
	}
	if recettes == nil {
		recettes = []database.Deployment{}
	}
	if pipelines == nil {
		pipelines = []database.Pipeline{}
	}

	respond(w, http.StatusOK, inventoryResponse{
		Summary: inventorySummary{
			Projects:   len(projects),
			Containers: len(containers),
			Recettes:   len(recettes),
			Pipelines:  len(pipelines),
		},
		Projects:   projects,
		Containers: containers,
		Recettes:   recettes,
		Pipelines:  pipelines,
		Live:       live,
	})
}
