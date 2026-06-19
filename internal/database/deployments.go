package database

import "time"

type Deployment struct {
	ID          int
	ProjectName string
	ContainerID string
	Image       string
	Port        int
	Status      string
	URL         string
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

func (db *DB) CreateDeployment(d *Deployment) error {
	return db.QueryRow(`
		INSERT INTO deployments (project_name, container_id, image, port, status, url)
		VALUES ($1,$2,$3,$4,$5,$6)
		RETURNING id, created_at`,
		d.ProjectName, d.ContainerID, d.Image, d.Port, d.Status, d.URL,
	).Scan(&d.ID, &d.CreatedAt)
}

func (db *DB) UpdateDeploymentStatus(containerID, status string) error {
	_, err := db.Exec(`
		UPDATE deployments SET status=$1, updated_at=NOW()
		WHERE container_id=$2`, status, containerID)
	return err
}

func (db *DB) ListDeployments() ([]Deployment, error) {
	rows, err := db.Query(`
		SELECT id, project_name, container_id, image, port, status, url, created_at
		FROM deployments ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var deployments []Deployment
	for rows.Next() {
		var d Deployment
		rows.Scan(&d.ID, &d.ProjectName, &d.ContainerID, &d.Image,
			&d.Port, &d.Status, &d.URL, &d.CreatedAt)
		deployments = append(deployments, d)
	}
	return deployments, nil
}
