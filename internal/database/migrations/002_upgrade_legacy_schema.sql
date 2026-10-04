-- Mise à niveau des bases créées par l'ancien schema.go : 001 utilise
-- CREATE TABLE IF NOT EXISTS et ne modifie donc pas les tables existantes.
-- Sans effet sur une base créée directement par 001.

-- projects
ALTER TABLE projects ADD COLUMN IF NOT EXISTS scm_provider VARCHAR(100);
ALTER TABLE projects ADD COLUMN IF NOT EXISTS ci_provider  VARCHAR(100);
ALTER TABLE projects ADD COLUMN IF NOT EXISTS reg_provider VARCHAR(100);
ALTER TABLE projects ADD COLUMN IF NOT EXISTS rt_provider  VARCHAR(100);
ALTER TABLE projects ADD COLUMN IF NOT EXISTS golden_path  VARCHAR(100);
ALTER TABLE projects ADD COLUMN IF NOT EXISTS created_by   VARCHAR(255) DEFAULT 'system';

-- pipelines
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS ci_provider VARCHAR(100);
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS web_url     VARCHAR(500);
ALTER TABLE pipelines ADD COLUMN IF NOT EXISTS ref         VARCHAR(100) DEFAULT 'main';

-- deployments : container_id renommé en deployment_id
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'deployments' AND column_name = 'container_id') THEN
        ALTER TABLE deployments RENAME COLUMN container_id TO deployment_id;
    END IF;
END $$;
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS rt_provider VARCHAR(100);
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS tag         VARCHAR(100) DEFAULT 'latest';
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS deployed_by VARCHAR(255) DEFAULT 'system';

-- audit_log : details passe de TEXT à JSONB (l'ancien texte devient une chaîne JSON)
DO $$
BEGIN
    IF EXISTS (SELECT 1 FROM information_schema.columns
               WHERE table_name = 'audit_log' AND column_name = 'details' AND data_type = 'text') THEN
        ALTER TABLE audit_log ALTER COLUMN details TYPE JSONB USING to_jsonb(details);
    END IF;
END $$;
ALTER TABLE audit_log ADD COLUMN IF NOT EXISTS ip_address VARCHAR(50);
