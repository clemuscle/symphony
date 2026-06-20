# Guidance schéma — tables de tâches

## Colonnes recommandées pour une table de tâche asynchrone

À confronter systématiquement à `internal/database/schema.go` réel avant
de proposer une migration — ce qui suit est un référentiel, pas un schéma
à imposer aveuglément s'il existe déjà une structure différente mais
cohérente.

| Colonne | Type suggéré | Justification |
|---|---|---|
| `id` | UUID ou serial | Identifiant interne Symphony |
| `task_type` | text/enum | `create_repo`, `trigger_pipeline`, `deploy`, `scale`... — permet de filtrer/grouper dans l'audit et l'UI |
| `project_id` / `service_name` | référence | Pour lier la tâche au projet/service concerné (`Catalogue.vue`, `Projects.vue` en ont besoin pour l'affichage) |
| `status` | text/enum | `pending`, `running`, `success`, `failed` — voir `state-diagram.md` pour les transitions valides |
| `external_ref` | text, nullable | ID retourné par le driver externe (ex: l'ID de pipeline GitLab retourné par `TriggerPipeline`). Indispensable pour interroger `GetPipelineStatus` au redémarrage ou en polling |
| `request_payload` | jsonb | La requête initiale complète — permet de rejouer/débugger sans deviner ce qui a été demandé |
| `result_payload` | jsonb, nullable | Le résultat final (URL de déploiement, erreur détaillée...) |
| `created_at` | timestamp | Horodatage de création |
| `updated_at` | timestamp | Dernière transition d'état — mis à jour à chaque changement de `status` |
| `error_detail` | text, nullable | Message d'erreur lisible si `status = failed` |

## Index recommandés dès la création

- Index sur `status` — filtrage fréquent ("toutes les tâches RUNNING au
  redémarrage", "toutes les tâches FAILED récentes" pour l'UI)
- Index sur `project_id`/`service_name` — affichage des tâches d'un
  projet donné
- Index sur `created_at` — tri chronologique pour l'audit et les vues
  `Deployments.vue`/`Projects.vue`

Ajouter ces index dès la création de table, pas en réaction à un
problème de performance constaté plus tard — le coût est nul à la
création, non nul en migration a posteriori sur une table déjà peuplée.

## Driver PostgreSQL : pgx, pas lib/pq

Le `go.mod` actuel importe `github.com/lib/pq` en indirect. Ce paquet
n'est plus maintenu activement par son auteur d'origine. Pour toute
nouvelle table ou requête, privilégier `github.com/jackc/pgx/v5` (et son
wrapper `database/sql` si la compatibilité `database/sql` standard est
souhaitée, ou l'API native `pgx` pour de meilleures performances et un
support actif des types PostgreSQL avancés comme `jsonb`).

Si une migration complète de `lib/pq` vers `pgx` n'est pas faite
immédiatement, au minimum : ne pas ajouter de nouveau code qui dépend
spécifiquement de `lib/pq`, pour ne pas alourdir la migration future.

## Migrations

`internal/database/schema.go` semble actuellement porter le schéma
directement (probablement des `CREATE TABLE IF NOT EXISTS` exécutés au
démarrage, à confirmer en lisant le fichier réel). Ça reste gérable pour
un nombre de tables faible. Si le schéma dépasse 5-6 migrations
distinctes dans le temps (ajouts de colonnes, nouvelles tables avec
dépendances), recommander un outil de migration versionné
(`golang-migrate` est un choix standard et léger, cohérent avec l'esprit
dépendances minimales du projet) plutôt que de continuer à faire évoluer
un fichier Go monolithique de DDL.
