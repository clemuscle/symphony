package database

import "time"

type Project struct {
	ID          int       `json:"id"`
	Name        string    `json:"name"`
	Description string    `json:"description"`
	Language    string    `json:"language"`
	Type        string    `json:"type"`
	Port        int       `json:"port"`
	Namespace   string    `json:"namespace"`
	RepoURL     string    `json:"repo_url"`
	RepoPath    string    `json:"repo_path"`
	RegistryURL string    `json:"registry_url"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (db *DB) CreateProject(p *Project) error {
	return db.QueryRow(`
		INSERT INTO projects (name, description, language, type, port, namespace, repo_url, repo_path, registry_url)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, created_at`,
		p.Name, p.Description, p.Language, p.Type, p.Port,
		p.Namespace, p.RepoURL, p.RepoPath, p.RegistryURL,
	).Scan(&p.ID, &p.CreatedAt)
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.Query(`SELECT id, name, description, language, type, port, namespace, repo_url, repo_path, registry_url, created_at FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		rows.Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type,
			&p.Port, &p.Namespace, &p.RepoURL, &p.RepoPath, &p.RegistryURL, &p.CreatedAt)
		projects = append(projects, p)
	}
	return projects, nil
}

func (db *DB) GetProject(name string) (*Project, error) {
	var p Project
	err := db.QueryRow(`SELECT id, name, description, language, type, port, namespace, repo_url, repo_path, registry_url, created_at FROM projects WHERE name=$1`, name).
		Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type,
			&p.Port, &p.Namespace, &p.RepoURL, &p.RepoPath, &p.RegistryURL, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
