package database

import (
	"encoding/json"
	"time"
)

type Project struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Language    string `json:"language"`
	Type        string `json:"type"`
	Port        int    `json:"port"`
	Namespace   string `json:"namespace"`
	SCMProvider string `json:"scm_provider"`
	CIProvider  string `json:"ci_provider"`
	RegProvider string `json:"registry_provider"`
	RTProvider  string `json:"runtime_provider"`
	RepoPath    string `json:"repo_path"`
	RepoURL     string `json:"repo_url"`
	RegistryURL string `json:"registry_url"`
	Stack       string `json:"stack"`
	// StackParameters : valeurs des paramètres de la stack, à la création
	StackParameters map[string]any `json:"stack_parameters"`
	Team            string         `json:"team"` // équipe propriétaire (groupe de l'annuaire)
	CreatedBy       string         `json:"created_by"`
	CreatedAt       time.Time      `json:"created_at"`
	// Deleting n'est pas en base : renseigné par l'API pendant une suppression en cours
	Deleting bool `json:"deleting,omitempty"`
}

func (db *DB) CreateProject(p *Project) error {
	return db.QueryRow(`
		INSERT INTO projects
			(name, description, language, type, port, namespace,
			 scm_provider, ci_provider, reg_provider, rt_provider,
			 repo_path, repo_url, registry_url, stack, stack_parameters, created_by, team)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17)
		RETURNING id, created_at`,
		p.Name, p.Description, p.Language, p.Type, p.Port, p.Namespace,
		p.SCMProvider, p.CIProvider, p.RegProvider, p.RTProvider,
		p.RepoPath, p.RepoURL, p.RegistryURL, p.Stack, jsonParams(p.StackParameters), p.CreatedBy, p.Team,
	).Scan(&p.ID, &p.CreatedAt)
}

// jsonParams : les valeurs des paramètres en JSON ({} sans paramètre)
func jsonParams(params map[string]any) []byte {
	if len(params) == 0 {
		return []byte("{}")
	}
	b, err := json.Marshal(params)
	if err != nil {
		return []byte("{}")
	}
	return b
}

// scanParams : relit stack_parameters
func scanParams(raw []byte) map[string]any {
	params := map[string]any{}
	json.Unmarshal(raw, &params)
	return params
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.Query(`
		SELECT id, name, COALESCE(description,''), COALESCE(language,''), COALESCE(type,''),
		       COALESCE(port,0), COALESCE(namespace,''),
		       COALESCE(scm_provider,''), COALESCE(ci_provider,''), COALESCE(reg_provider,''), COALESCE(rt_provider,''),
		       COALESCE(repo_path,''), COALESCE(repo_url,''), COALESCE(registry_url,''),
		       COALESCE(stack,''), stack_parameters, COALESCE(created_by,''), team, created_at
		FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		var params []byte
		if err := rows.Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type, &p.Port,
			&p.Namespace, &p.SCMProvider, &p.CIProvider, &p.RegProvider, &p.RTProvider,
			&p.RepoPath, &p.RepoURL, &p.RegistryURL, &p.Stack, &params, &p.CreatedBy, &p.Team, &p.CreatedAt); err != nil {
			return nil, err
		}
		p.StackParameters = scanParams(params)
		projects = append(projects, p)
	}
	return projects, rows.Err()
}

func (db *DB) GetProjectByName(name string) (*Project, error) {
	var p Project
	var params []byte
	err := db.QueryRow(`
		SELECT id, name, COALESCE(description,''), COALESCE(language,''), COALESCE(type,''),
		       COALESCE(port,0), COALESCE(namespace,''),
		       COALESCE(scm_provider,''), COALESCE(ci_provider,''), COALESCE(reg_provider,''), COALESCE(rt_provider,''),
		       COALESCE(repo_path,''), COALESCE(repo_url,''), COALESCE(registry_url,''),
		       COALESCE(stack,''), stack_parameters, COALESCE(created_by,''), team, created_at
		FROM projects WHERE name=$1`, name).
		Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type, &p.Port,
			&p.Namespace, &p.SCMProvider, &p.CIProvider, &p.RegProvider, &p.RTProvider,
			&p.RepoPath, &p.RepoURL, &p.RegistryURL, &p.Stack, &params, &p.CreatedBy, &p.Team, &p.CreatedAt)
	if err != nil {
		return nil, err
	}
	p.StackParameters = scanParams(params)
	return &p, nil
}

// GetProjectByRepoPath : le projet dont le repo est repoPath
func (db *DB) GetProjectByRepoPath(repoPath string) (*Project, error) {
	var name string
	if err := db.QueryRow(`SELECT name FROM projects WHERE repo_path=$1`, repoPath).Scan(&name); err != nil {
		return nil, err
	}
	return db.GetProjectByName(name)
}

// DeleteProject supprime le projet et l'historique de ses pipelines
// (pipelines.project_name contient le chemin du repo). L'audit est conservé.
func (db *DB) DeleteProject(name, repoPath string) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	if _, err := tx.Exec(`DELETE FROM pipelines WHERE project_name=$1`, repoPath); err != nil {
		return err
	}
	if _, err := tx.Exec(`DELETE FROM projects WHERE name=$1`, name); err != nil {
		return err
	}
	return tx.Commit()
}
