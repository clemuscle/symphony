-- Tâches asynchrones (création, déploiement, suppression, actions Day-2) :
-- PENDING → RUNNING → SUCCESS | FAILED. L'étape en cours et ses résultats
-- sont enregistrés à chaque pas : une tâche reprend après un redémarrage.
CREATE TABLE IF NOT EXISTS tasks (
    id           BIGSERIAL PRIMARY KEY,
    kind         VARCHAR(50)  NOT NULL,
    resource     VARCHAR(255) NOT NULL,
    status       VARCHAR(20)  NOT NULL DEFAULT 'PENDING',
    steps        JSONB        NOT NULL DEFAULT '[]',  -- libellés des étapes
    step         INT          NOT NULL DEFAULT 0,     -- index de l'étape en cours
    detail       TEXT         NOT NULL DEFAULT '',    -- avancement de l'étape en cours
    params       JSONB        NOT NULL DEFAULT '{}',  -- entrée (jamais renvoyée par l'API)
    result       JSONB        NOT NULL DEFAULT '{}',  -- sorties, enregistrées étape par étape
    error        TEXT         NOT NULL DEFAULT '',
    wait         JSONB,                               -- pipeline attendue, sinon NULL
    created_by   VARCHAR(255) NOT NULL DEFAULT 'system',
    created_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    updated_at   TIMESTAMPTZ  NOT NULL DEFAULT NOW(),
    finished_at  TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_tasks_unfinished ON tasks(status) WHERE status IN ('PENDING', 'RUNNING');
CREATE INDEX IF NOT EXISTS idx_tasks_resource ON tasks(resource, created_at DESC);
CREATE INDEX IF NOT EXISTS idx_tasks_created ON tasks(created_at DESC);
