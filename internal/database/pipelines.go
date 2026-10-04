package database

import "time"

type Pipeline struct {
	ID          int       `json:"id"`
	ProjectName string    `json:"project_name"`
	PipelineID  string    `json:"pipeline_id"`
	CIProvider  string    `json:"ci_provider"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	WebURL      string    `json:"web_url"`
	Ref         string    `json:"ref"`
	TriggeredBy string    `json:"triggered_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (db *DB) CreatePipeline(p *Pipeline) error {
	return db.QueryRow(`
		INSERT INTO pipelines (project_name, pipeline_id, ci_provider, type, status, web_url, ref, triggered_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8)
		RETURNING id, created_at`,
		p.ProjectName, p.PipelineID, p.CIProvider, p.Type, p.Status,
		p.WebURL, p.Ref, p.TriggeredBy,
	).Scan(&p.ID, &p.CreatedAt)
}

func (db *DB) UpdatePipelineStatus(pipelineID, status string) error {
	_, err := db.Exec(`UPDATE pipelines SET status=$1, updated_at=NOW() WHERE pipeline_id=$2`,
		status, pipelineID)
	return err
}

func (db *DB) ListPipelines(projectName string, limit int) ([]Pipeline, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, ci_provider, type, status, web_url, ref, triggered_by, created_at, updated_at
		FROM pipelines WHERE project_name=$1
		ORDER BY created_at DESC LIMIT $2`, projectName, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pipelines []Pipeline
	for rows.Next() {
		var p Pipeline
		rows.Scan(&p.ID, &p.ProjectName, &p.PipelineID, &p.CIProvider, &p.Type,
			&p.Status, &p.WebURL, &p.Ref, &p.TriggeredBy, &p.CreatedAt, &p.UpdatedAt)
		pipelines = append(pipelines, p)
	}
	return pipelines, nil
}
