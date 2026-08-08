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
	// OwnerGroup — groupe Symphony propriétaire du projet (voir
	// config/rbac.yaml project_groups), "" pour un projet créé avant cette
	// fonctionnalité ou tant qu'un admin ne l'a pas adopté explicitement.
	OwnerGroup string    `json:"owner_group,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

const projectColumns = `id, name, description, language, type, port, namespace, repo_url, repo_path, registry_url, status, owner_group, created_at`

func scanProject(row interface{ Scan(...any) error }, p *Project) error {
	var ownerGroup *string
	if err := row.Scan(&p.ID, &p.Name, &p.Description, &p.Language, &p.Type,
		&p.Port, &p.Namespace, &p.RepoURL, &p.RepoPath, &p.RegistryURL, &p.Status, &ownerGroup, &p.CreatedAt); err != nil {
		return err
	}
	if ownerGroup != nil {
		p.OwnerGroup = *ownerGroup
	}
	return nil
}

func (db *DB) CreateProject(p *Project) error {
	return db.QueryRow(`
		INSERT INTO projects (name, description, language, type, port, namespace, repo_url, repo_path, registry_url, owner_group)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10)
		RETURNING id, status, created_at`,
		p.Name, p.Description, p.Language, p.Type, p.Port,
		p.Namespace, p.RepoURL, p.RepoPath, p.RegistryURL, nilIfEmpty(p.OwnerGroup),
	).Scan(&p.ID, &p.Status, &p.CreatedAt)
}

func (db *DB) ListProjects() ([]Project, error) {
	rows, err := db.Query(`SELECT ` + projectColumns + ` FROM projects ORDER BY created_at DESC`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := scanProject(rows, &p); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

// ListProjectsForGroups retourne uniquement les projets dont owner_group
// figure dans groups — filtrage en SQL (pas en Go après un ListProjects()
// complet), c'est ce qui rend la visibilité par groupe scalable. groups vide
// retourne une liste vide, jamais tous les projets — l'appelant doit détecter
// ce cas et appeler ListProjects() explicitement s'il veut le comportement
// non scopé (ex: admin, ou scoping désactivé).
func (db *DB) ListProjectsForGroups(groups []string) ([]Project, error) {
	if len(groups) == 0 {
		return nil, nil
	}
	rows, err := db.Query(`SELECT `+projectColumns+` FROM projects WHERE owner_group = ANY($1) ORDER BY created_at DESC`, groups)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var projects []Project
	for rows.Next() {
		var p Project
		if err := scanProject(rows, &p); err != nil {
			return nil, err
		}
		projects = append(projects, p)
	}
	return projects, nil
}

func (db *DB) GetProject(name string) (*Project, error) {
	var p Project
	row := db.QueryRow(`SELECT `+projectColumns+` FROM projects WHERE name=$1`, name)
	if err := scanProject(row, &p); err != nil {
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
	row := db.QueryRow(`SELECT `+projectColumns+` FROM projects WHERE repo_path=$1`, repoPath)
	if err := scanProject(row, &p); err != nil {
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

// UpdateProjectOwnerGroup transfère la propriété d'un projet vers un autre
// groupe — réservé à un admin (voir PUT /api/v1/projects/{name}/group). Ne
// modifie que cette ligne : ne déplace ni le repo ni le namespace GitLab,
// qui restent où le provisioning initial les a créés (principe #3 —
// déplacer un repo serait de l'exécution sur un système vivant, jamais fait
// directement par Symphony).
func (db *DB) UpdateProjectOwnerGroup(name, group string) error {
	_, err := db.Exec(`UPDATE projects SET owner_group=$1, updated_at=NOW() WHERE name=$2`, nilIfEmpty(group), name)
	return err
}
