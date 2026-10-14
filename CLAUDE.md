# CLAUDE.md

Symphony est une Internal Developer Platform : backend Go, SPA Vue 3, au-dessus de GitLab
(ou GitHub), de leur CI, du registry GitLab, de Docker et d'Argo CD (Kubernetes en
GitOps). Projet de **portfolio** visant des recruteurs tech et des leads DevOps.

- Architecture et décisions : [documentation/architecture.md](documentation/architecture.md)
- Plan par versions (où on en est, ce qui vient) : [ROADMAP.md](ROADMAP.md)

## Commandes

```bash
go build ./...                  # -gcflags=-e pour voir toutes les erreurs
go vet ./...
go test -race ./...             # tests unitaires (aucun ne demande GitLab ou Docker)
go run ./cmd/symphony           # depuis la racine du repo
cd frontend && npm run dev      # front en dev (npm install d'abord), API sur :8080
cd frontend && npm run build    # compile le front dans internal/web/dist, embarqué au prochain go build
docker build -t symphony .      # image complète (front + binaire), ~10 Mo
./demo/up.sh / ./demo/down.sh   # démo complète (GitLab CE, runner, Postgres, Keycloak, k3s + Argo CD, Symphony)
```

## Pour lancer le serveur

- **Bootstrap** `symphony.yaml` à la racine (`SYMPHONY_CONFIG` pour un autre chemin) : sa
  section `config_source` dit où lire la vraie configuration, `root/symphony-config` →
  `symphony.yaml` sur GitLab. Sans `config_source`, le fichier local est lu comme une
  configuration complète (hors ligne). `${VAR}` est remplacé par l'environnement.
  En démo, la config est dans `platform/symphony-config`.
- **`.env`** à la racine (godotenv) : `GITLAB_TOKEN`, `GITHUB_TOKEN`, `DB_HOST`/`DB_PORT`/
  `DB_NAME`/`DB_USER`/`DB_PASSWORD` (défauts `localhost`/`5432`/`symphony`/`symphony`/
  `symphony123`), `PORT` (8080).
- **PostgreSQL** (conteneur `symphony-postgres`), **GitLab** sur `http://gitlab.local:8929`,
  **socket Docker** ; le daemon Docker doit déclarer `gitlab.local:5050` en insecure registry.
- Front : servi par le binaire (`internal/web`, `go:embed`) ; sans `npm run build`, `/`
  affiche une page d'aide. Il appelle toujours l'API sur sa propre origine : en dev,
  Vite relaie `/api` et `/auth` vers `http://localhost:8080` (cookie de session).
  `VITE_API_URL` (`frontend/.env.local`) vise une autre instance.
- Le serveur s'arrête si la configuration est illisible au démarrage ou s'il n'y a pas de
  provider SCM par défaut.

## Repères dans le code

- `cmd/symphony` : `config.go` (bootstrap, source de config, `configWatcher` qui recharge
  à chaud), `providers.go` (`buildProviders` : un `case` par type de provider), `main.go`.
- `internal/provider` : interfaces (`interfaces.go`) et `Registry`. Ajouter un type de
  provider = implémenter l'interface + un `case` dans `providers.go`, enveloppé par
  `instrumented.SCM/CI/Registry/Runtime` (mesures pour `/metrics`).
- `internal/auth` : OIDC en Backend-for-Frontend (`/auth/login`, `/auth/callback`,
  `/auth/logout`), session en cookie signé, rôles, CSRF, CORS. Le `Holder` porte la
  politique courante et est remplacé au rechargement de la config (`buildPolicy`).
- `internal/metrics` : métriques Prometheus (`/metrics`, middleware HTTP, `Reload`).
- `internal/tasks` : moteur de tâches (étapes, `Exec.Set` pour les résultats repris au
  redémarrage, `WaitPipeline`, sondage, `Hub` SSE). Les types de tâches et leurs étapes
  sont dans `internal/api/handlers/tasks.go` (`RegisterTasks`) ; les handlers valident puis
  `submit` (202). `internal/webhook` : `/webhooks/gitlab`.
- `internal/gitops` : `Syncer` (catalogue : la fiche `catalog-info.yaml` à la racine du
  repo de **chaque projet en base**, `catalogue.file`) et `TemplateLoader` (golden paths,
  `templates/<nom>/template.yaml` de `symphony-templates`). Format des templates et des
  fiches : README de ce repo. La fiche est générée à la création (`catalogueFile` dans
  `handlers/projects.go`) et poussée avec le squelette.
- `internal/provider/runtime/argocd` : runtime Kubernetes GitOps. Commite
  `apps/<service>/` (Deployment, Service, Ingress, `kustomization.yaml`) dans le repo de
  déploiement via le SCM `scm_ref`, upsert de l'`Application` Argo CD, refresh ; Stop et
  Restart réécrivent `kustomization.yaml`. Jamais d'appel à l'API Kubernetes.
- `demo/` : `docker-compose.yml`, `up.sh` (démarre et initialise tout, rejouable,
  `--no-k8s` sans k3s ni Argo CD, `--no-seed` sans projets de démo), `seed.sh` (crée et
  déploie les projets de `seed/projects.json` par l'API, au nom de bob, puis commite leur
  fiche complète `seed/<nom>.yaml`),
  `down.sh` (`--purge` efface les données), `SCENARIO.md`. `demo/gitlab/` est le contenu
  poussé dans le groupe `platform` de la GitLab de démo (`symphony-config`,
  `symphony-templates`, `symphony-deployments`) ; les projets vont dans le groupe `apps`
  (`config.namespace` du SCM). GitLab se connecte au realm Keycloak (client `gitlab`,
  `discovery: false` : navigateur sur localhost, jetons sur keycloak:8080) ; `up.sh` crée
  les comptes et leurs droits (CE ne synchronise pas les groupes). Le mettre à jour
  quand on change les golden paths de référence. `demo/capture/` régénère
  `documentation/demo.gif` (Playwright).
- `internal/api` : toutes les routes dans `server.go`, chacune avec son rôle minimum
  (`r.With(viewer|developer|admin)`), un fichier de handlers par domaine ;
  ce qui n'est pas une route d'API va au front (`web.Handler`, `index.html` pour les routes Vue).
  Chaque appel de `frontend/src/api.js` a sa route.

## Pièges

- Une nouvelle route `/api/v1` doit déclarer son rôle (`r.With(...)`) : sans lui, tout
  utilisateur connecté, même sans rôle, y aurait accès. Les handlers lisent
  l'utilisateur avec `auth.UserFrom(ctx)` ; `audit(db, r, ...)` et `userID(r)` l'utilisent.
- Sans section `auth` dans la config, l'authentification est **désactivée** (tout le monde
  est admin, avertissement au démarrage) : c'est le cas de la config de l'utilisateur.
  `SESSION_SECRET` (32 caractères min.) garde les sessions d'un redémarrage à l'autre.
- `auth.issuer` est l'URL vue par le navigateur ; `auth.discovery_url` l'URL interne
  (Symphony en conteneur). Keycloak : `KC_HOSTNAME` + `KC_HOSTNAME_BACKCHANNEL_DYNAMIC`.
- Deux « registry » : `provider.Registry` contient tous les providers ;
  `RegistryProvider` est le registry d'**images** (`providers.GetRegistry(id)`).
- La page **Projets** (accueil) montre les projets en base avec leur fiche
  (`GET /projects`, champ `service`) ; le **Catalogue** du front, ce sont les golden paths.
  Plus de fiche sans projet : `catalogue.repo`/`path` ne sont plus lus (avertissement).
- Le contenu de `provider.Registry` est **remplacé d'un bloc** au rechargement de la
  config. Ne jamais garder un provider dans une struct longue durée : le résoudre à chaque
  appel (`providers.SCM("")`), comme `Syncer` et `TemplateLoader`.
- Un `GoldenPath` chargé garde son `repo`/`branch` : on lit son skeleton là d'où il vient,
  même si la config a changé de repo de templates.
- Runtime `argocd` : `repo_url` est l'URL de clone **vue par Argo CD** (en démo
  `http://k3s:<port>` via le relais `gitlab-proxy`), `repo` le chemin pour le SCM. Aucun
  identifiant de registry dans Git : le cluster a les siens (`demo/k3s/registries.yaml`).
  Argo CD répond 403 (et non 404) pour une Application absente.
- Une étape de tâche peut être **rejouée** après un redémarrage : la rendre idempotente
  (tester `x.Get(...)` avant d'agir, `x.Set(...)` juste après). Pas de secret dans
  `x.Set` (renvoyé par l'API) ; `Params` n'est jamais renvoyé.
- Webhooks : `webhooks.url` = Symphony vu par GitLab ; GitLab doit autoriser les requêtes
  vers le réseau local (`up.sh` le fait). Sans section `webhooks`, sondage seul (15 s).
- `ListDeployments` agrège tous les runtimes ; chaque déploiement porte `provider`, que le
  front renvoie (`?provider=`) pour Stop/Restart/Logs.
- Port d'un déploiement : `port` = port de l'appli **dans** le conteneur, `host_port` = port
  publié (0 = celui du conteneur remplacé, sinon un port libre).
- Suppression de projet asynchrone (202), complète : `Remove` sur **chaque** runtime
  (conteneurs et images Docker ; Application Argo CD, ses ressources et ses manifests),
  lignes `deployments`, repo, fiche catalogue. GitLab refuse de supprimer un projet tant
  que la purge de son registry n'est pas finie (2–3 min), puis ne fait que le
  **programmer** (GitLab 18 : `-deletion_scheduled-`) : `DeleteRepo` le supprime ensuite
  pour de bon (`permanently_remove`, par l'id numérique).
- Le front lit exactement les clés JSON de l'API : toute struct sérialisée a des tags
  `json` en snake_case.
- `resolveInputs` refuse les entrées d'action non déclarées (anti-injection de variables
  CI) : ne pas assouplir.
- Une méthode ajoutée à une interface de provider doit aussi l'être dans
  `internal/provider/instrumented` : sinon l'embarquement la transmet sans la mesurer.
- `/metrics` est public tant que l'authentification (v0.3) n'existe pas.
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
- Tout se fait sur `main` (pas de branche de version) ; pousser seulement quand c'est
  demandé. La branche `legacy` sur GitHub est l'ancien historique du projet, gardé en
  archive : ne pas y toucher.
- Les versions sont des **tags** : cocher la roadmap, compléter `CHANGELOG.md`, tag annoté
  `vX.Y` poussé (la CI publie `ghcr.io/clemuscle/symphony:X.Y` et `:latest`), puis
  `gh release create`.

## Conventions de test de bout en bout

- Serveur de test : binaire construit dans le scratchpad, lancé avec `PORT=18080` (le 8080
  peut être pris par l'instance de l'utilisateur), arrêté après.
- Projets temporaires nommés `symphony-*-e2e`, supprimés après via
  `DELETE /api/v1/projects/{nom}`, qui supprime aussi leurs déploiements.
- Modifications temporaires d'un repo de config ou de templates : restaurer l'original et
  vérifier qu'il est identique.
- Démo : la tester sur des ports décalés pour ne pas gêner la GitLab de l'utilisateur
  (`GITLAB_PORT=18929 REGISTRY_PORT=15050 GITLAB_SSH_PORT=12224 SYMPHONY_PORT=18090
  KEYCLOAK_PORT=18180 K8S_HTTP_PORT=18081 ARGOCD_PORT=18088 K8S_API_PORT=16443`).
  Jeton pour l'API : grant `password` du client `symphony` sur
  Keycloak (`username=bob&password=bob`) ; parcours navigateur avec Playwright,
  `SYMPHONY_IMAGE=symphony:local` pour une image construite localement, puis
  `./demo/down.sh --purge` (~5,5 Go de RAM avec k3s). `down.sh` ne supprime que les conteneurs dont
  l'image vient du registry de la démo.
