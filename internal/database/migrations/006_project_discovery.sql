-- v0.14 : projets découverts dans un groupe GitLab ou une organisation GitHub
-- (catalogue.discovery). discovered_from : la source (<scm>/<groupe>) ; vide :
-- projet créé par Symphony. Retirer un projet découvert ne touche ni à son
-- repo ni à ses images.
ALTER TABLE projects ADD COLUMN IF NOT EXISTS discovered_from TEXT NOT NULL DEFAULT '';
-- Branche lue (fiche, manifests) et lancée (pipelines, actions) : celle par
-- défaut du repo
ALTER TABLE projects ADD COLUMN IF NOT EXISTS branch TEXT NOT NULL DEFAULT 'main';
