package database

import "time"

// Deployment suit l'état d'un déploiement délégué à un pipeline CI.
// Machine d'états :
//
//	pending → running  (pipeline success)
//	pending → failed   (pipeline failed)    [terminal]
//	pending → stopped  (pipeline canceled)  [terminal]
//	running → stopped  (destroy déclenché)  [terminal]
type Deployment struct {
	ID          int       `json:"id"`
	ProjectName string    `json:"project_name"`
	PipelineID  string    `json:"pipeline_id"`
	Image       string    `json:"image"`
	Port        int       `json:"port"`
	Status      string    `json:"status"`
	URL         string    `json:"url"`
	RecetteName string    `json:"recette_name,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (db *DB) CreateDeployment(d *Deployment) error {
	return db.QueryRow(`
		INSERT INTO deployments (project_name, pipeline_id, image, port, status, url, recette_name)
		VALUES ($1,$2,$3,$4,$5,$6,$7)
		RETURNING id, created_at`,
		d.ProjectName, d.PipelineID, d.Image, d.Port, d.Status, d.URL, nilIfEmpty(d.RecetteName),
	).Scan(&d.ID, &d.CreatedAt)
}

func (db *DB) GetDeploymentByID(id string) (*Deployment, error) {
	var d Deployment
	var recetteName *string
	err := db.QueryRow(`
		SELECT id, project_name, pipeline_id, image, port, status, url, recette_name, created_at
		FROM deployments WHERE id=$1`, id).
		Scan(&d.ID, &d.ProjectName, &d.PipelineID, &d.Image,
			&d.Port, &d.Status, &d.URL, &recetteName, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	if recetteName != nil {
		d.RecetteName = *recetteName
	}
	return &d, nil
}

func (db *DB) GetRecette(projectName, recetteName string) (*Deployment, error) {
	var d Deployment
	var rn *string
	err := db.QueryRow(`
		SELECT id, project_name, pipeline_id, image, port, status, url, recette_name, created_at
		FROM deployments WHERE project_name=$1 AND recette_name=$2
		ORDER BY created_at DESC LIMIT 1`, projectName, recetteName).
		Scan(&d.ID, &d.ProjectName, &d.PipelineID, &d.Image,
			&d.Port, &d.Status, &d.URL, &rn, &d.CreatedAt)
	if err != nil {
		return nil, err
	}
	if rn != nil {
		d.RecetteName = *rn
	}
	return &d, nil
}

// UpdateDeploymentStatus tente de transitionner le déploiement vers le statut
// donné. La mise à jour est ignorée si le déploiement est déjà dans un état
// terminal (failed, stopped) — une notification tardive ou désordonnée ne doit
// jamais rouvrir un déploiement terminé.
func (db *DB) UpdateDeploymentStatus(pipelineID, status string) (bool, error) {
	res, err := db.Exec(`
		UPDATE deployments SET status=$1, updated_at=NOW()
		WHERE pipeline_id=$2 AND status NOT IN ('failed', 'stopped')`, status, pipelineID)
	if err != nil {
		return false, err
	}
	rows, err := res.RowsAffected()
	return rows > 0, err
}

func (db *DB) ListPendingDeployments() ([]Deployment, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, image, port, status, url, recette_name, created_at
		FROM deployments WHERE status='pending' ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		var recetteName *string
		rows.Scan(&d.ID, &d.ProjectName, &d.PipelineID, &d.Image,
			&d.Port, &d.Status, &d.URL, &recetteName, &d.CreatedAt)
		if recetteName != nil {
			d.RecetteName = *recetteName
		}
		deployments = append(deployments, d)
	}
	return deployments, nil
}

func (db *DB) ListDeployments() ([]Deployment, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, image, port, status, url, recette_name, created_at
		FROM deployments WHERE recette_name IS NULL ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		var recetteName *string
		rows.Scan(&d.ID, &d.ProjectName, &d.PipelineID, &d.Image,
			&d.Port, &d.Status, &d.URL, &recetteName, &d.CreatedAt)
		deployments = append(deployments, d)
	}
	return deployments, nil
}

func (db *DB) ListRecettes(projectName string) ([]Deployment, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, image, port, status, url, recette_name, created_at
		FROM deployments WHERE project_name=$1 AND recette_name IS NOT NULL
		ORDER BY created_at DESC`, projectName)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var recettes []Deployment
	for rows.Next() {
		var d Deployment
		var recetteName *string
		rows.Scan(&d.ID, &d.ProjectName, &d.PipelineID, &d.Image,
			&d.Port, &d.Status, &d.URL, &recetteName, &d.CreatedAt)
		if recetteName != nil {
			d.RecetteName = *recetteName
		}
		recettes = append(recettes, d)
	}
	return recettes, nil
}

// ListAllActiveDeployments retourne les déploiements prod en cours (pas stopped/failed).
func (db *DB) ListAllActiveDeployments() ([]Deployment, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, image, port, status, url, recette_name, created_at
		FROM deployments
		WHERE recette_name IS NULL AND status NOT IN ('stopped', 'failed')
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deployment
	for rows.Next() {
		var d Deployment
		var rn *string
		rows.Scan(&d.ID, &d.ProjectName, &d.PipelineID, &d.Image,
			&d.Port, &d.Status, &d.URL, &rn, &d.CreatedAt)
		out = append(out, d)
	}
	return out, nil
}

// ListAllActiveRecettes retourne toutes les recettes en cours (pas stopped/failed) tous projets.
func (db *DB) ListAllActiveRecettes() ([]Deployment, error) {
	rows, err := db.Query(`
		SELECT id, project_name, pipeline_id, image, port, status, url, recette_name, created_at
		FROM deployments
		WHERE recette_name IS NOT NULL AND status NOT IN ('stopped', 'failed')
		ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Deployment
	for rows.Next() {
		var d Deployment
		var rn *string
		rows.Scan(&d.ID, &d.ProjectName, &d.PipelineID, &d.Image,
			&d.Port, &d.Status, &d.URL, &rn, &d.CreatedAt)
		if rn != nil {
			d.RecetteName = *rn
		}
		out = append(out, d)
	}
	return out, nil
}

func nilIfEmpty(s string) *string {
	if s == "" {
		return nil
	}
	return &s
}
