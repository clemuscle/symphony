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
	SCMProvider string    `json:"scm_provider"`
	CIProvider  string    `json:"ci_provider"`
	RegProvider string    `json:"registry_provider"`
	RTProvider  string    `json:"runtime_provider"`
	RepoPath    string    `json:"repo_path"`
	RepoURL     string    `json:"repo_url"`
	RegistryURL string    `json:"registry_url"`
	GoldenPath  string    `json:"golden_path"`
	CreatedBy   string    `json:"created_by"`
	CreatedAt   time.Time `json:"created_at"`
}

func (db *DB) CreateProject(p *Project) error {
	return db.QueryRow(`
		INSERT INTO projects
			(name, description, language, type, port, namespace,
			 scm_provider, ci_provider, reg_provider, rt_provider,
			 repo_path, repo_url, registry_url, golden_path, created_by)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15)
		RETURNING id, created_at`,
		p.Name, p.Description, p.Language, p.Type, p.Port, p.Namespace,
		p.SCMProvider, p.CIProvider, p.RegProvider, p.RTProvider,
		p.RepoPath, p.RepoURL, p.RegistryURL, p.GoldenPath, p.CreatedBy,
	).Scan(&p.ID, &p.CreatedAt)
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.Query(`
		SELECT id, name, description, language, type, port, namespace,
		       scm_provider, ci_provider, reg_provider, rt_provider,
		       repo_path, repo_url, registry_url, golden_path, created_by, created_at
		FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		rows.Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type, &p.Port,
			&p.Namespace, &p.SCMProvider, &p.CIProvider, &p.RegProvider, &p.RTProvider,
			&p.RepoPath, &p.RepoURL, &p.RegistryURL, &p.GoldenPath, &p.CreatedBy, &p.CreatedAt)
		projects = append(projects, p)
	}
	return projects, nil
}

func (db *DB) GetProjectByName(name string) (*Project, error) {
	var p Project
	err := db.QueryRow(`
		SELECT id, name, description, language, type, port, namespace,
		       scm_provider, ci_provider, reg_provider, rt_provider,
		       repo_path, repo_url, registry_url, golden_path, created_by, created_at
		FROM projects WHERE name=$1`, name).
		Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type, &p.Port,
			&p.Namespace, &p.SCMProvider, &p.CIProvider, &p.RegProvider, &p.RTProvider,
			&p.RepoPath, &p.RepoURL, &p.RegistryURL, &p.GoldenPath, &p.CreatedBy, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}
