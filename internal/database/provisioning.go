package database

import (
	"database/sql"
	"time"
)

type ProvisioningStep struct {
	ID          int       `json:"id"`
	ProjectName string    `json:"project_name"`
	Step        string    `json:"step"`
	Status      string    `json:"status"`
	ErrorDetail string    `json:"error_detail,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UpsertProvisioningStep enregistre l'état d'une étape de provisioning pour
// un projet. Chaque étape de createProject est exécutée une seule fois de
// façon synchrone dans la requête, donc pas de garde de transition à faire
// ici (contrairement à UpdatePipelineStatus) — il n'y a pas de notification
// tardive ou désordonnée à ignorer dans ce flux.
func (db *DB) UpsertProvisioningStep(projectName, step, status, errorDetail string) error {
	_, err := db.Exec(`
		INSERT INTO provisioning_steps (project_name, step, status, error_detail)
		VALUES ($1,$2,$3,$4)
		ON CONFLICT (project_name, step)
		DO UPDATE SET status=$3, error_detail=$4, updated_at=NOW()`,
		projectName, step, status, errorDetail)
	return err
}

func (db *DB) ListProvisioningSteps(projectName string) ([]ProvisioningStep, error) {
	rows, err := db.Query(`
		SELECT id, project_name, step, status, error_detail, created_at, updated_at
		FROM provisioning_steps WHERE project_name=$1
		ORDER BY id ASC`, projectName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var steps []ProvisioningStep
	for rows.Next() {
		var s ProvisioningStep
		var errDetail sql.NullString
		if err := rows.Scan(&s.ID, &s.ProjectName, &s.Step, &s.Status, &errDetail, &s.CreatedAt, &s.UpdatedAt); err != nil {
			return nil, err
		}
		s.ErrorDetail = errDetail.String
		steps = append(steps, s)
	}
	return steps, nil
}
