package database

import "time"

// ListAllDeploymentsSince returns every deployment (prod + recettes) created
// on or after `since`. Includes updated_at for runtime duration estimation.
func (db *DB) ListAllDeploymentsSince(since time.Time) ([]Deployment, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, image, port, status, url,
		       recette_name, created_at, updated_at
		FROM deployments
		WHERE created_at >= $1
		ORDER BY created_at DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Deployment
	for rows.Next() {
		var d Deployment
		var rn *string
		if err := rows.Scan(
			&d.ID, &d.ProjectName, &d.PipelineID, &d.Image, &d.Port,
			&d.Status, &d.URL, &rn, &d.CreatedAt, &d.UpdatedAt,
		); err != nil {
			return nil, err
		}
		if rn != nil {
			d.RecetteName = *rn
		}
		out = append(out, d)
	}
	return out, nil
}

// ListAllPipelinesSince returns every pipeline created on or after `since`.
// Includes updated_at for duration estimation.
func (db *DB) ListAllPipelinesSince(since time.Time) ([]Pipeline, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, type, status, triggered_by,
		       created_at, updated_at
		FROM pipelines
		WHERE created_at >= $1
		ORDER BY created_at DESC`, since)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []Pipeline
	for rows.Next() {
		var p Pipeline
		if err := rows.Scan(
			&p.ID, &p.ProjectName, &p.PipelineID, &p.Type,
			&p.Status, &p.TriggeredBy, &p.CreatedAt, &p.UpdatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, nil
}
