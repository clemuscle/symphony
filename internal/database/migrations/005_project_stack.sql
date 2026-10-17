-- v0.12 : les golden paths s'appellent désormais des stacks. Les valeurs des
-- paramètres choisies à la création sont gardées avec le projet.
ALTER TABLE projects RENAME COLUMN golden_path TO stack;
ALTER TABLE projects ADD COLUMN stack_parameters JSONB NOT NULL DEFAULT '{}';
