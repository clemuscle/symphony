package database

func (db *DB) Migrate() error {
	schema := `
	CREATE TABLE IF NOT EXISTS projects (
		id          SERIAL PRIMARY KEY,
		name        VARCHAR(255) NOT NULL UNIQUE,
		description TEXT,
		language    VARCHAR(50),
		type        VARCHAR(50),
		port        INT,
		namespace   VARCHAR(255),
		repo_url    VARCHAR(500),
		repo_path   VARCHAR(500),
		registry_url VARCHAR(500),
		status      VARCHAR(50) NOT NULL DEFAULT 'provisioning',
		created_at  TIMESTAMP DEFAULT NOW(),
		updated_at  TIMESTAMP DEFAULT NOW()
	);
	ALTER TABLE projects ADD COLUMN IF NOT EXISTS status VARCHAR(50) NOT NULL DEFAULT 'provisioning';

	CREATE TABLE IF NOT EXISTS provisioning_steps (
		id           SERIAL PRIMARY KEY,
		project_name VARCHAR(255) NOT NULL,
		step         VARCHAR(50) NOT NULL,
		status       VARCHAR(50) NOT NULL DEFAULT 'pending',
		error_detail TEXT,
		created_at   TIMESTAMP DEFAULT NOW(),
		updated_at   TIMESTAMP DEFAULT NOW(),
		UNIQUE (project_name, step)
	);
	CREATE INDEX IF NOT EXISTS idx_provisioning_steps_project_name ON provisioning_steps (project_name);

	CREATE TABLE IF NOT EXISTS pipelines (
		id           SERIAL PRIMARY KEY,
		project_name VARCHAR(255) NOT NULL,
		pipeline_id  VARCHAR(100),
		type         VARCHAR(50),
		status       VARCHAR(50) DEFAULT 'pending',
		triggered_by VARCHAR(255) DEFAULT 'symphony',
		created_at   TIMESTAMP DEFAULT NOW(),
		updated_at   TIMESTAMP DEFAULT NOW()
	);

	-- États valides : pending → running | failed | stopped
	--   pending : pipeline CI déclenché, résultat attendu
	--   running : pipeline success, app en cours d'exécution
	--   failed  : pipeline échoué
	--   stopped : destroy pipeline déclenché (destroy-deploy / destroy-recette)
	CREATE TABLE IF NOT EXISTS deployments (
		id            SERIAL PRIMARY KEY,
		project_name  VARCHAR(255) NOT NULL,
		pipeline_id   VARCHAR(100),
		image         VARCHAR(500),
		port          INT,
		status        VARCHAR(50) DEFAULT 'pending',
		url           VARCHAR(500),
		recette_name  TEXT,
		created_at    TIMESTAMP DEFAULT NOW(),
		updated_at    TIMESTAMP DEFAULT NOW()
	);
	ALTER TABLE deployments ADD COLUMN IF NOT EXISTS recette_name TEXT;
	DO $$
	BEGIN
		IF EXISTS (SELECT 1 FROM information_schema.columns
		           WHERE table_name='deployments' AND column_name='container_id') THEN
			ALTER TABLE deployments RENAME COLUMN container_id TO pipeline_id;
		END IF;
	END $$;

	CREATE TABLE IF NOT EXISTS audit_log (
		id         SERIAL PRIMARY KEY,
		action     VARCHAR(100) NOT NULL,
		resource   VARCHAR(255),
		details    TEXT,
		user_id    VARCHAR(255) DEFAULT 'system',
		created_at TIMESTAMP DEFAULT NOW()
	);
	`
	_, err := db.Exec(schema)
	return err
}
