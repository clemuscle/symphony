package database

import "time"

type Pipeline struct {
	ID          int
	ProjectName string
	PipelineID  string
	Type        string
	Status      string
	TriggeredBy string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (db *DB) CreatePipeline(p *Pipeline) error {
	return db.QueryRow(`
		INSERT INTO pipelines (project_name, pipeline_id, type, status, triggered_by)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, created_at`,
		p.ProjectName, p.PipelineID, p.Type, p.Status, p.TriggeredBy,
	).Scan(&p.ID, &p.CreatedAt)
}

func (db *DB) UpdatePipelineStatus(pipelineID, status string) error {
	_, err := db.Exec(`
		UPDATE pipelines SET status=$1, updated_at=NOW()
		WHERE pipeline_id=$2`, status, pipelineID)
	return err
}

func (db *DB) ListPipelines(projectName string) ([]Pipeline, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, type, status, triggered_by, created_at
		FROM pipelines WHERE project_name=$1
		ORDER BY created_at DESC LIMIT 20`, projectName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var pipelines []Pipeline
	for rows.Next() {
		var p Pipeline
		rows.Scan(&p.ID, &p.ProjectName, &p.PipelineID, &p.Type,
			&p.Status, &p.TriggeredBy, &p.CreatedAt)
		pipelines = append(pipelines, p)
	}
	return pipelines, nil
}
