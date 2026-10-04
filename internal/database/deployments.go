package database

import "time"

type Deployment struct {
	ID           int       `json:"id"`
	ProjectName  string    `json:"project_name"`
	DeploymentID string    `json:"deployment_id"`
	RTProvider   string    `json:"runtime_provider"`
	Image        string    `json:"image"`
	Tag          string    `json:"tag"`
	Port         int       `json:"port"`
	Status       string    `json:"status"`
	URL          string    `json:"url"`
	DeployedBy   string    `json:"deployed_by"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (db *DB) CreateDeployment(d *Deployment) error {
	return db.QueryRow(`
		INSERT INTO deployments (project_name, deployment_id, rt_provider, image, tag, port, status, url, deployed_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at`,
		d.ProjectName, d.DeploymentID, d.RTProvider, d.Image, d.Tag,
		d.Port, d.Status, d.URL, d.DeployedBy,
	).Scan(&d.ID, &d.CreatedAt)
}

func (db *DB) UpdateDeploymentStatus(deploymentID, status string) error {
	_, err := db.Exec(`UPDATE deployments SET status=$1, updated_at=NOW() WHERE deployment_id=$2`,
		status, deploymentID)
	return err
}

func (db *DB) ListDeployments() ([]Deployment, error) {
	rows, err := db.Query(`
		SELECT id, project_name, deployment_id, COALESCE(rt_provider,''), COALESCE(image,''), COALESCE(tag,''),
		       COALESCE(port,0), COALESCE(status,''), COALESCE(url,''), COALESCE(deployed_by,''), created_at
		FROM deployments ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		if err := rows.Scan(&d.ID, &d.ProjectName, &d.DeploymentID, &d.RTProvider, &d.Image,
			&d.Tag, &d.Port, &d.Status, &d.URL, &d.DeployedBy, &d.CreatedAt); err != nil {
			return nil, err
		}
		deployments = append(deployments, d)
	}
	return deployments, rows.Err()
}
