-- Équipe propriétaire d'un projet (groupe de l'annuaire) : ses membres, et les
-- admins, le déploient et lancent ses actions. Vide pour les projets antérieurs :
-- réservés aux admins.
ALTER TABLE projects ADD COLUMN IF NOT EXISTS team TEXT NOT NULL DEFAULT '';
