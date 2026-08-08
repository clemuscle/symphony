package api

import (
	"net/http"
	"sort"
	"time"

	"github.com/yourorg/symphony/internal/database"
)

type projectCost struct {
	Project        string  `json:"project"`
	Namespace      string  `json:"namespace"`
	OwnerGroup     string  `json:"owner_group,omitempty"`
	ContainerHours float64 `json:"container_hours"`
	ContainerCost  float64 `json:"container_cost"`
	CIMinutes      float64 `json:"ci_minutes"`
	CICost         float64 `json:"ci_cost"`
	Total          float64 `json:"total"`
}

// teamCost agrège par owner_group (le "groupe" au sens produit — voir
// question ouverte n°2 de CLAUDE.md), pas par namespace Kubernetes/Docker :
// ce sont deux notions d'équipe différentes, namespace resterait ambigu dès
// qu'un groupe possède plusieurs projets dans des namespaces distincts.
type teamCost struct {
	Group         string  `json:"group"`
	ContainerCost float64 `json:"container_cost"`
	CICost        float64 `json:"ci_cost"`
	Total         float64 `json:"total"`
}

type costsResponse struct {
	Period    string        `json:"period"`    // "YYYY-MM"
	Currency  string        `json:"currency"`
	Total     float64       `json:"total"`
	ByProject []projectCost `json:"by_project"`
	ByTeam    []teamCost    `json:"by_team"`
}

func (s *Server) getCosts(w http.ResponseWriter, r *http.Request) {
	// Période : mois courant par défaut, ?month=YYYY-MM pour naviguer
	month := r.URL.Query().Get("month")
	var since time.Time
	var periodLabel string
	if month != "" {
		t, err := time.Parse("2006-01", month)
		if err != nil {
			respond(w, http.StatusBadRequest, map[string]string{"error": "format invalide, attendu YYYY-MM"})
			return
		}
		since = t
		periodLabel = month
	} else {
		now := time.Now()
		since = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
		periodLabel = now.Format("2006-01")
	}

	groups, unrestricted := s.scopeGroups(r)

	var deployments []database.Deployment
	var pipelines []database.Pipeline
	var err error
	if unrestricted {
		deployments, err = s.db.ListAllDeploymentsSince(since)
	} else {
		deployments, err = s.db.ListAllDeploymentsSinceForGroups(since, groups)
	}
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	if unrestricted {
		pipelines, err = s.db.ListAllPipelinesSince(since)
	} else {
		pipelines, err = s.db.ListAllPipelinesSinceForGroups(since, groups)
	}
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Récupérer namespace + owner_group des projets pour le regroupement par équipe
	projects, err := s.listProjectsScoped(r)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	nsOf := map[string]string{}    // project name → namespace
	groupOf := map[string]string{} // project name → owner_group
	for _, p := range projects {
		nsOf[p.Name] = p.Namespace
		groupOf[p.Name] = p.OwnerGroup
	}

	rates := s.costCfg.Rates
	now := time.Now()

	pc := map[string]*projectCost{} // project name → cost

	getPC := func(name string) *projectCost {
		if _, ok := pc[name]; !ok {
			pc[name] = &projectCost{Project: name, Namespace: nsOf[name], OwnerGroup: groupOf[name]}
		}
		return pc[name]
	}

	// Container hours
	for _, d := range deployments {
		end := now
		if d.Status == "stopped" || d.Status == "failed" {
			end = d.UpdatedAt
		}
		hours := end.Sub(d.CreatedAt).Hours()
		if hours < 0 {
			hours = 0
		}
		c := getPC(d.ProjectName)
		c.ContainerHours += hours
		c.ContainerCost += hours * rates.ContainerHourly
	}

	// CI minutes
	for _, p := range pipelines {
		end := now
		if p.Status == "success" || p.Status == "failed" || p.Status == "canceled" {
			end = p.UpdatedAt
		}
		minutes := end.Sub(p.CreatedAt).Minutes()
		if minutes < 0 {
			minutes = 0
		}
		c := getPC(p.ProjectName)
		c.CIMinutes += minutes
		c.CICost += minutes * rates.CIMinute
	}

	// Totaux par projet
	byProject := make([]projectCost, 0, len(pc))
	var grandTotal float64
	for _, c := range pc {
		c.ContainerHours = round2(c.ContainerHours)
		c.ContainerCost = round2(c.ContainerCost)
		c.CIMinutes = round2(c.CIMinutes)
		c.CICost = round2(c.CICost)
		c.Total = round2(c.ContainerCost + c.CICost)
		grandTotal += c.Total
		byProject = append(byProject, *c)
	}
	sort.Slice(byProject, func(i, j int) bool {
		return byProject[i].Total > byProject[j].Total
	})

	// Agrégat par équipe (owner_group — voir teamCost)
	teamMap := map[string]*teamCost{}
	for _, c := range byProject {
		g := c.OwnerGroup
		if g == "" {
			g = "(sans groupe)"
		}
		if _, ok := teamMap[g]; !ok {
			teamMap[g] = &teamCost{Group: g}
		}
		teamMap[g].ContainerCost += c.ContainerCost
		teamMap[g].CICost += c.CICost
		teamMap[g].Total += c.Total
	}
	byTeam := make([]teamCost, 0, len(teamMap))
	for _, t := range teamMap {
		t.ContainerCost = round2(t.ContainerCost)
		t.CICost = round2(t.CICost)
		t.Total = round2(t.Total)
		byTeam = append(byTeam, *t)
	}
	sort.Slice(byTeam, func(i, j int) bool {
		return byTeam[i].Total > byTeam[j].Total
	})

	respond(w, http.StatusOK, costsResponse{
		Period:    periodLabel,
		Currency:  s.costCfg.Currency,
		Total:     round2(grandTotal),
		ByProject: byProject,
		ByTeam:    byTeam,
	})
}

func round2(f float64) float64 {
	return float64(int(f*100+0.5)) / 100
}
