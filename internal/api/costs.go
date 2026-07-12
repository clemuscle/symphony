package api

import (
	"net/http"
	"sort"
	"time"
)

type projectCost struct {
	Project         string  `json:"project"`
	Namespace       string  `json:"namespace"`
	ContainerHours  float64 `json:"container_hours"`
	ContainerCost   float64 `json:"container_cost"`
	CIMinutes       float64 `json:"ci_minutes"`
	CICost          float64 `json:"ci_cost"`
	Total           float64 `json:"total"`
}

type teamCost struct {
	Namespace      string  `json:"namespace"`
	ContainerCost  float64 `json:"container_cost"`
	CICost         float64 `json:"ci_cost"`
	Total          float64 `json:"total"`
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

	deployments, err := s.db.ListAllDeploymentsSince(since)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	pipelines, err := s.db.ListAllPipelinesSince(since)
	if err != nil {
		respond(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	// Récupérer les namespaces des projets pour le regroupement par équipe
	projects, _ := s.db.ListProjects()
	nsOf := map[string]string{} // project name → namespace
	for _, p := range projects {
		nsOf[p.Name] = p.Namespace
	}

	rates := s.costCfg.Rates
	now := time.Now()

	pc := map[string]*projectCost{} // project name → cost

	getPC := func(name string) *projectCost {
		if _, ok := pc[name]; !ok {
			pc[name] = &projectCost{Project: name, Namespace: nsOf[name]}
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

	// Agrégat par équipe (namespace)
	teamMap := map[string]*teamCost{}
	for _, c := range byProject {
		ns := c.Namespace
		if ns == "" {
			ns = "(sans équipe)"
		}
		if _, ok := teamMap[ns]; !ok {
			teamMap[ns] = &teamCost{Namespace: ns}
		}
		teamMap[ns].ContainerCost += c.ContainerCost
		teamMap[ns].CICost += c.CICost
		teamMap[ns].Total += c.Total
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
