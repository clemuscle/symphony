-- Projets créés via Symphony
CREATE TABLE IF NOT EXISTS projects (
    id            SERIAL PRIMARY KEY,
    name          VARCHAR(255) NOT NULL UNIQUE,
    description   TEXT,
    language      VARCHAR(50),
    type          VARCHAR(50),
    port          INT DEFAULT 8080,
    namespace     VARCHAR(255),
    scm_provider  VARCHAR(100),
    ci_provider   VARCHAR(100),
    reg_provider  VARCHAR(100),
    rt_provider   VARCHAR(100),
    repo_path     VARCHAR(500),
    repo_url      VARCHAR(500),
    registry_url  VARCHAR(500),
    golden_path   VARCHAR(100),
    created_by    VARCHAR(255) DEFAULT 'system',
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

-- Pipelines déclenchés
CREATE TABLE IF NOT EXISTS pipelines (
    id            SERIAL PRIMARY KEY,
    project_name  VARCHAR(255) NOT NULL,
    pipeline_id   VARCHAR(100) NOT NULL,
    ci_provider   VARCHAR(100),
    type          VARCHAR(50) DEFAULT 'manual',
    status        VARCHAR(50) DEFAULT 'pending',
    web_url       VARCHAR(500),
    ref           VARCHAR(100) DEFAULT 'main',
    triggered_by  VARCHAR(255) DEFAULT 'system',
    created_at    TIMESTAMPTZ DEFAULT NOW(),
    updated_at    TIMESTAMPTZ DEFAULT NOW()
);

-- Déploiements runtime
CREATE TABLE IF NOT EXISTS deployments (
    id             SERIAL PRIMARY KEY,
    project_name   VARCHAR(255) NOT NULL,
    deployment_id  VARCHAR(255) NOT NULL,
    rt_provider    VARCHAR(100),
    image          VARCHAR(500),
    tag            VARCHAR(100) DEFAULT 'latest',
    port           INT,
    status         VARCHAR(50) DEFAULT 'running',
    url            VARCHAR(500),
    deployed_by    VARCHAR(255) DEFAULT 'system',
    created_at     TIMESTAMPTZ DEFAULT NOW(),
    updated_at     TIMESTAMPTZ DEFAULT NOW()
);

-- Audit log
CREATE TABLE IF NOT EXISTS audit_log (
    id          SERIAL PRIMARY KEY,
    action      VARCHAR(100) NOT NULL,
    resource    VARCHAR(255),
    details     JSONB,
    user_id     VARCHAR(255) DEFAULT 'system',
    ip_address  VARCHAR(50),
    created_at  TIMESTAMPTZ DEFAULT NOW()
);

-- Index utiles
CREATE INDEX IF NOT EXISTS idx_projects_name ON projects(name);
CREATE INDEX IF NOT EXISTS idx_pipelines_project ON pipelines(project_name);
CREATE INDEX IF NOT EXISTS idx_pipelines_status ON pipelines(status);
CREATE INDEX IF NOT EXISTS idx_deployments_project ON deployments(project_name);
CREATE INDEX IF NOT EXISTS idx_deployments_status ON deployments(status);
CREATE INDEX IF NOT EXISTS idx_audit_action ON audit_log(action);
CREATE INDEX IF NOT EXISTS idx_audit_created ON audit_log(created_at DESC);

-- Table migrations pour tracker les versions
CREATE TABLE IF NOT EXISTS schema_migrations (
    version     VARCHAR(50) PRIMARY KEY,
    applied_at  TIMESTAMPTZ DEFAULT NOW()
);
