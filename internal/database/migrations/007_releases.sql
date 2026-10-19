-- v0.15 : la table deployments devient l'historique des déploiements de
-- chaque projet, par environnement : d'où vient la version (déploiement,
-- promotion, retour arrière), qui l'a approuvée, et la tâche qui l'a faite.
-- status : deploying, running (le service a démarré), failed, stopped.
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS environment TEXT NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS origin TEXT NOT NULL DEFAULT 'deploy';
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS promoted_from TEXT NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS approved_by TEXT NOT NULL DEFAULT '';
ALTER TABLE deployments ADD COLUMN IF NOT EXISTS task_id BIGINT;
CREATE INDEX IF NOT EXISTS idx_deployments_env ON deployments(project_name, environment, created_at DESC);
