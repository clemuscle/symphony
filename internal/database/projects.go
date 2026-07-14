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
	Status      string    `json:"status"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

func (db *DB) CreateProject(p *Project) error {
	return db.QueryRow(`
		INSERT INTO projects (name, description, language, type, port, namespace, repo_url, repo_path, registry_url)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9)
		RETURNING id, status, created_at`,
		p.Name, p.Description, p.Language, p.Type, p.Port,
		p.Namespace, p.RepoURL, p.RepoPath, p.RegistryURL,
	).Scan(&p.ID, &p.Status, &p.CreatedAt)
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.Query(`SELECT id, name, description, language, type, port, namespace, repo_url, repo_path, registry_url, status, created_at FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type,
			&p.Port, &p.Namespace, &p.RepoURL, &p.RepoPath, &p.RegistryURL, &p.Status, &p.CreatedAt); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (db *DB) GetProject(name string) (*Project, error) {
	var p Project
	err := db.QueryRow(`SELECT id, name, description, language, type, port, namespace, repo_url, repo_path, registry_url, status, created_at FROM projects WHERE name=$1`, name).
		Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type,
			&p.Port, &p.Namespace, &p.RepoURL, &p.RepoPath, &p.RegistryURL, &p.Status, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

// GetProjectByRepoPath retrouve un projet par son repo_path (ex: "groupe/mon-app")
// plutôt que son nom interne — utilisé pour vérifier qu'un project_path fourni
// par un appelant correspond bien à un projet que Symphony a provisionné, avant
// de déclencher un pipeline avec le token de service dessus.
func (db *DB) GetProjectByRepoPath(repoPath string) (*Project, error) {
	var p Project
	err := db.QueryRow(`SELECT id, name, description, language, type, port, namespace, repo_url, repo_path, registry_url, status, created_at FROM projects WHERE repo_path=$1`, repoPath).
		Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type,
			&p.Port, &p.Namespace, &p.RepoURL, &p.RepoPath, &p.RegistryURL, &p.Status, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	return &p, nil
}

func (db *DB) UpdateProjectStatus(name, status string) error {
	_, err := db.Exec(`UPDATE projects SET status=$1, updated_at=NOW() WHERE name=$2`, status, name)
	return err
}

func (db *DB) UpdateProjectRegistryURL(name, registryURL string) error {
	_, err := db.Exec(`UPDATE projects SET registry_url=$1, updated_at=NOW() WHERE name=$2`, registryURL, name)
	return err
}
