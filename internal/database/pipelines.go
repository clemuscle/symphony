package database

import "time"

type Pipeline struct {
	ID          int       `json:"id"`
	ProjectName string    `json:"project_name"`
	PipelineID  string    `json:"pipeline_id"`
	Type        string    `json:"type"`
	Status      string    `json:"status"`
	TriggeredBy string    `json:"triggered_by"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (db *DB) CreatePipeline(p *Pipeline) error {
	return db.QueryRow(`
		INSERT INTO pipelines (project_name, pipeline_id, type, status, triggered_by)
		VALUES ($1,$2,$3,$4,$5)
		RETURNING id, created_at`,
		p.ProjectName, p.PipelineID, p.Type, p.Status, p.TriggeredBy,
	).Scan(&p.ID, &p.CreatedAt)
}

// UpdatePipelineStatus tente de transitionner le pipeline vers le statut
// donné. La mise à jour est ignorée (pas une erreur) si le pipeline est
// déjà dans un état final (success/failed) : une notification de statut
// tardive ou désordonnée ne doit jamais rouvrir une tâche déjà terminée.
// applied indique si la transition a effectivement été appliquée.
func (db *DB) UpdatePipelineStatus(pipelineID, status string) (applied bool, err error) {
	res, err := db.Exec(`
		UPDATE pipelines SET status=$1, updated_at=NOW()
		WHERE pipeline_id=$2 AND status NOT IN ('success', 'failed')`, status, pipelineID)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	return rows > 0, nil
}

func (db *DB) ListPendingPipelines() ([]Pipeline, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, type, status, triggered_by, created_at
		FROM pipelines WHERE status='pending' ORDER BY created_at`)
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

// ListActivePipelines retourne les pipelines en état non-terminal (pending/running).
func (db *DB) ListActivePipelines() ([]Pipeline, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, type, status, triggered_by, created_at
		FROM pipelines WHERE status NOT IN ('success', 'failed', 'canceled')
		ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pipeline
	for rows.Next() {
		var p Pipeline
		rows.Scan(&p.ID, &p.ProjectName, &p.PipelineID, &p.Type,
			&p.Status, &p.TriggeredBy, &p.CreatedAt)
		out = append(out, p)
	}
	return out, nil
}

// ListAllRecentPipelines retourne les 50 derniers pipelines tous projets confondus.
// Utilisé par la page Projets pour pré-charger l'historique au mount sans N+1.
func (db *DB) ListAllRecentPipelines() ([]Pipeline, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, type, status, triggered_by, created_at
		FROM pipelines ORDER BY created_at DESC LIMIT 50`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Pipeline
	for rows.Next() {
		var p Pipeline
		rows.Scan(&p.ID, &p.ProjectName, &p.PipelineID, &p.Type,
			&p.Status, &p.TriggeredBy, &p.CreatedAt)
		out = append(out, p)
	}
	return out, nil
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
