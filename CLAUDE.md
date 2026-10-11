# CLAUDE.md

Symphony est une Internal Developer Platform : backend Go, SPA Vue 3, au-dessus de GitLab
(ou GitHub), de leur CI, du registry GitLab et de Docker. Projet de **portfolio** visant des
recruteurs tech et des leads DevOps.

- Architecture et décisions : [documentation/architecture.md](documentation/architecture.md)
- Plan par versions (où on en est, ce qui vient) : [ROADMAP.md](ROADMAP.md)

## Commandes

```bash
go build ./...                  # -gcflags=-e pour voir toutes les erreurs
go vet ./...
go test -race ./...             # tests unitaires (aucun ne demande GitLab ou Docker)
go run ./cmd/symphony           # depuis la racine du repo
cd frontend && npm run dev      # front (npm install d'abord) ; npx vite build pour vérifier
```

## Pour lancer le serveur

- **Bootstrap** `symphony.yaml` à la racine (`SYMPHONY_CONFIG` pour un autre chemin) : sa
  section `config_source` dit où lire la vraie configuration, `root/symphony-config` →
  `symphony.yaml` sur GitLab. Sans `config_source`, le fichier local est lu comme une
  configuration complète (hors ligne). `${VAR}` est remplacé par l'environnement.
- **`.env`** à la racine (godotenv) : `GITLAB_TOKEN`, `GITHUB_TOKEN`, `DB_HOST`/`DB_PORT`/
  `DB_NAME`/`DB_USER`/`DB_PASSWORD` (défauts `localhost`/`5432`/`symphony`/`symphony`/
  `symphony123`), `PORT` (8080), `CATALOGUE_BRANCH` (surcharge `catalogue.branch`).
- **PostgreSQL** (conteneur `symphony-postgres`), **GitLab** sur `http://gitlab.local:8929`,
  **socket Docker** ; le daemon Docker doit déclarer `gitlab.local:5050` en insecure registry.
- Front : l'URL de l'API vient de `VITE_API_URL` au build (`frontend/.env.local`, ignoré
  par git), `http://localhost:8080` par défaut, vide = même origine.
- Le serveur s'arrête si la configuration est illisible au démarrage ou s'il n'y a pas de
  provider SCM par défaut.

## Repères dans le code

- `cmd/symphony` : `config.go` (bootstrap, source de config, `configWatcher` qui recharge
  à chaud), `providers.go` (`buildProviders` : un `case` par type de provider), `main.go`.
- `internal/provider` : interfaces (`interfaces.go`) et `Registry`. Ajouter un type de
  provider = implémenter l'interface + un `case` dans `providers.go`.
- `internal/gitops` : `Syncer` (catalogue, `services/` du repo de config) et
  `TemplateLoader` (golden paths, `templates/<nom>/template.yaml` de `symphony-templates`).
  Format des templates : README de ce repo.
- `internal/api` : toutes les routes dans `server.go`, un fichier de handlers par domaine.
  Chaque appel de `frontend/src/api.js` a sa route.

## Pièges

- Deux « registry » : `provider.Registry` contient tous les providers ;
  `RegistryProvider` est le registry d'**images** (`providers.GetRegistry(id)`).
- Le contenu de `provider.Registry` est **remplacé d'un bloc** au rechargement de la
  config. Ne jamais garder un provider dans une struct longue durée : le résoudre à chaque
  appel (`providers.SCM("")`), comme `Syncer` et `TemplateLoader`.
- Un `GoldenPath` chargé garde son `repo`/`branch` : on lit son skeleton là d'où il vient,
  même si la config a changé de repo de templates.
- Port d'un déploiement : `port` = port de l'appli **dans** le conteneur, `host_port` = port
  publié (0 = celui du conteneur remplacé, sinon un port libre).
- Suppression de projet asynchrone (202) : GitLab refuse de supprimer un projet tant que la
  purge de son registry n'est pas finie (2–3 min).
- Le front lit exactement les clés JSON de l'API : toute struct sérialisée a des tags
  `json` en snake_case.
- `resolveInputs` refuse les entrées d'action non déclarées (anti-injection de variables
  CI) : ne pas assouplir.
- Migrations SQL : créer `migrations/00N_*.sql`, ajouter sa variable `//go:embed` **et**
  l'ajouter à la liste dans `db.go`.

## Conventions

- Commentaires, logs, erreurs d'API, docs et messages de commit **en français**.
- Pas de secret dans les fichiers : un provider nomme sa variable (`config.token_env`).
  GitLab CI et le registry GitLab essaient `token_env`, puis `<scm_ref>_TOKEN`, puis
  `GITLAB_TOKEN` (`gitlabToken`).
- Les handlers qui modifient quelque chose écrivent dans l'audit (`db.LogAudit`).
- Code formaté avec `gofmt` (vérifié par la CI).

## Règles de travail

- **Demander avant d'écrire dans GitLab** (`symphony-config`, `symphony-templates`, repos
  de services) ; montrer d'abord un dry-run de ce qui sera poussé.
- Tester de bout en bout contre le vrai GitLab quand un changement touche les providers,
  le catalogue, les templates ou la config, en plus des tests unitaires.
- Mettre à jour ce fichier, `documentation/architecture.md` et `ROADMAP.md` (cocher les
  cases) avec le code.
- Commits locaux sur `main` ; pousser seulement quand c'est demandé.

## Conventions de test de bout en bout

- Serveur de test : binaire construit dans le scratchpad, lancé avec `PORT=18080` (le 8080
  peut être pris par l'instance de l'utilisateur), arrêté après.
- Projets temporaires nommés `symphony-*-e2e`, supprimés après via
  `DELETE /api/v1/projects/{nom}` (GitLab les montre ensuite en `-deletion_scheduled-` :
  normal). Déploiements de test : supprimer le conteneur et ses lignes `deployments`.
- Modifications temporaires d'un repo de config ou de templates : restaurer l'original et
  vérifier qu'il est identique.
