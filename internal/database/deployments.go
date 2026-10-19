package database

import "time"

// Origine d'une version déployée
const (
	OriginDeploy    = "deploy"    // déploiement demandé (image et tag choisis)
	OriginPromotion = "promotion" // image saine de l'environnement précédent
	OriginRollback  = "rollback"  // retour à une version déjà passée dans l'environnement
)

// Statuts d'une version de l'historique
const (
	ReleaseDeploying = "deploying"
	ReleaseRunning   = "running" // le service a démarré
	ReleaseFailed    = "failed"
	ReleaseStopped   = "stopped"
)

// Deployment : une version déployée d'un projet dans un environnement
// (historique des déploiements : la matrice, le retour arrière)
type Deployment struct {
	ID           int       `json:"id"`
	ProjectName  string    `json:"project_name"`
	Environment  string    `json:"environment"`
	DeploymentID string    `json:"deployment_id"`
	RTProvider   string    `json:"runtime_provider"`
	Image        string    `json:"image"`
	Tag          string    `json:"tag"`
	Port         int       `json:"port"`
	Status       string    `json:"status"`
	URL          string    `json:"url"`
	Origin       string    `json:"origin"`
	PromotedFrom string    `json:"promoted_from,omitempty"`
	DeployedBy   string    `json:"deployed_by"`
	ApprovedBy   string    `json:"approved_by,omitempty"`
	TaskID       int64     `json:"task_id,omitempty"`
	CreatedAt    time.Time `json:"created_at"`
	UpdatedAt    time.Time `json:"updated_at"`
}

func (db *DB) CreateDeployment(d *Deployment) error {
	if d.Origin == "" {
		d.Origin = OriginDeploy
	}
	var taskID any
	if d.TaskID != 0 {
		taskID = d.TaskID
	}
	return db.QueryRow(`
		INSERT INTO deployments (project_name, environment, deployment_id, rt_provider, image, tag, port,
		                         status, url, origin, promoted_from, deployed_by, approved_by, task_id)
		VALUES ($1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14)
		RETURNING id, created_at, updated_at`,
		d.ProjectName, d.Environment, d.DeploymentID, d.RTProvider, d.Image, d.Tag, d.Port,
		d.Status, d.URL, d.Origin, d.PromotedFrom, d.DeployedBy, d.ApprovedBy, taskID,
	).Scan(&d.ID, &d.CreatedAt, &d.UpdatedAt)
}

// SetReleaseStatus : statut de la version id de l'historique
func (db *DB) SetReleaseStatus(id int, status string) error {
	_, err := db.Exec(`UPDATE deployments SET status=$1, updated_at=NOW() WHERE id=$2`, status, id)
	return err
}

// UpdateDeploymentStatus : statut de la dernière version du déploiement
// deploymentID (arrêt depuis l'UI) ; les versions précédentes gardent le leur
func (db *DB) UpdateDeploymentStatus(deploymentID, status string) error {
	_, err := db.Exec(`UPDATE deployments SET status=$1, updated_at=NOW()
		WHERE id = (SELECT MAX(id) FROM deployments WHERE deployment_id=$2)`,
		status, deploymentID)
	return err
}

// ReleaseFilter : Project vide = tous les projets, Environment nil = tous
// les environnements ; Latest : la dernière version de chaque projet dans
// chaque environnement seulement
type ReleaseFilter struct {
	Project     string
	Environment *string
	Latest      bool
	Limit       int
}

const releaseColumns = `id, project_name, environment, deployment_id, COALESCE(rt_provider,''), COALESCE(image,''),
	COALESCE(tag,''), COALESCE(port,0), COALESCE(status,''), COALESCE(url,''), origin, promoted_from,
	COALESCE(deployed_by,''), approved_by, COALESCE(task_id,0), created_at, COALESCE(updated_at, created_at)`

// ListReleases : l'historique des déploiements, les plus récents d'abord
func (db *DB) ListReleases(f ReleaseFilter) ([]Deployment, error) {
	if f.Limit <= 0 {
		f.Limit = 50
	}
	env, anyEnv := "", f.Environment == nil
	if !anyEnv {
		env = *f.Environment
	}
	where := `($1 = '' OR project_name = $1) AND ($2 OR environment = $3)`
	if f.Latest {
		where += ` AND id IN (SELECT MAX(id) FROM deployments GROUP BY project_name, environment)`
	}
	return db.queryReleases(where+` ORDER BY id DESC LIMIT $4`, f.Project, anyEnv, env, f.Limit)
}

// GetRelease : la version id de l'historique, nil si elle n'existe pas
func (db *DB) GetRelease(id int) (*Deployment, error) {
	list, err := db.queryReleases(`id = $1`, id)
	if err != nil || len(list) == 0 {
		return nil, err
	}
	return &list[0], nil
}

func (db *DB) queryReleases(where string, args ...any) ([]Deployment, error) {
	rows, err := db.Query(`SELECT `+releaseColumns+` FROM deployments WHERE `+where, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	list := []Deployment{}
	for rows.Next() {
		var d Deployment
		if err := rows.Scan(&d.ID, &d.ProjectName, &d.Environment, &d.DeploymentID, &d.RTProvider, &d.Image,
			&d.Tag, &d.Port, &d.Status, &d.URL, &d.Origin, &d.PromotedFrom,
			&d.DeployedBy, &d.ApprovedBy, &d.TaskID, &d.CreatedAt, &d.UpdatedAt); err != nil {
			return nil, err
		}
		list = append(list, d)
	}
	return list, rows.Err()
}

// DeleteDeployments efface l'historique des déploiements d'un projet
func (db *DB) DeleteDeployments(projectName string) error {
	_, err := db.Exec(`DELETE FROM deployments WHERE project_name=$1`, projectName)
	return err
}
