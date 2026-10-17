# CLAUDE.md

Symphony est une Internal Developer Platform (backend Go, SPA Vue 3) au-dessus d'outils
existants, chacun derrière une interface de provider : SCM (GitLab, GitHub), CI (GitLab CI,
GitHub Actions), registry d'images (GitLab, GHCR), runtime (Docker, Argo CD), secrets
(Vault, env), identité (Keycloak, OIDC). Né comme projet de **portfolio**, il vise
désormais une v1.0 adoptable par de vraies équipes : le produit passe avant la vitrine.

- Cap et décisions produit : [documentation/vision.md](documentation/vision.md)
- Architecture et décisions : [documentation/architecture.md](documentation/architecture.md)
- Où on en est, ce qui vient : [ROADMAP.md](ROADMAP.md)

## Commandes

```bash
go build ./... && go vet ./... && go test -race ./...   # aucun test ne demande GitLab ni Docker
SYMPHONY_TEST_DB=1 DB_PORT=55432 go test -race ./...   # + tests sur PostgreSQL (dbtest), dont l'intégration
go run ./cmd/symphony             # depuis la racine ; lit symphony.yaml (bootstrap) et .env
go run ./cmd/symphony validate -config <fichier> -stacks <dossier>   # hors ligne, code 1 si erreur
cd frontend && npm run dev        # front en dev, relaie /api et /auth vers :8080
cd frontend && npm run typecheck && npm run lint && npm test   # vue-tsc, ESLint, Vitest
cd frontend && npm run build      # front compilé dans internal/web/dist, embarqué au go build
docker build -t symphony .        # image complète (~15 Mo)
helm lint deploy/helm/symphony    # chart ; la CI l'installe sur kind (deploy/helm/e2e)
./demo/up.sh / ./demo/down.sh     # démo complète ; --no-k8s, --no-seed ; down.sh --purge
cd quickstart && docker compose up -d   # Symphony + PostgreSQL sur une forge existante (.env)
SYMPHONY_TEST_K8S=1 go test -run TestCNPGOnCluster ./internal/provider/runtime/argocd/   # infra sur un cluster avec CloudNativePG (CI : kind)
```

## Lancer le serveur

- `symphony.yaml` local = **bootstrap** (`SYMPHONY_CONFIG` pour un autre chemin) : sa section
  `config_source` dit où lire la vraie configuration dans Git ; sans elle, il est lu comme
  une configuration complète. `${VAR}` est remplacé par l'environnement ; le bootstrap peut
  déclarer ses `providers.secrets`.
- `.env` à la racine : jetons, `DB_*` (défauts `localhost`/`5432`/`symphony`/`symphony`/
  `symphony123`), `PORT` (8080). PostgreSQL et le socket Docker sont requis.
- Sans section `auth`, l'authentification est **désactivée** (tout le monde admin,
  avertissement). `SESSION_SECRET` (32 caractères min.) garde les sessions.
- Le serveur s'arrête si la configuration est illisible au démarrage, si un secret est
  introuvable ou s'il n'y a pas de provider SCM par défaut.

## Repères dans le code

- `cmd/symphony` : `config.go` (bootstrap, `configWatcher` : rechargement à chaud, relecture
  des secrets, `Status` pour la page Administration), `secrets.go` (références `secret://`
  résolues dans une copie), `providers.go` (`buildProviders` : secrets d'abord, puis un
  `case` par type), `validate.go` (`symphony validate` : la même construction, secrets
  remplacés par des substituts), `main.go`.
- `internal/provider` : interfaces (`interfaces.go`), `Registry`, `secretref.go`, `check.go`
  (aides des méthodes `Check`), une implémentation par dossier (`scm/`, `ci/`, `registry/`,
  `runtime/`, `secrets/`, `identity/`), et `instrumented/` qui les enveloppe pour
  `/metrics` et retient leur dernier appel.
- `internal/api` : toutes les routes dans `server.go`, un fichier de handlers par domaine.
  Les types de tâches : `handlers/tasks.go` (`RegisterTasks`, `submit`, routes des tâches),
  leurs étapes dans `task_create.go`, `task_deploy.go`, `task_delete.go`, `task_action.go` ;
  un handler valide puis `submit` (202). Page Administration : `handlers/admin.go`
  (`AdminStatus`, `CheckProviders`).
- `internal/tasks` (moteur de tâches, attente de pipelines, `Hub` SSE), `internal/auth` (OIDC
  en Backend-for-Frontend, rôles, CSRF), `internal/gitops` (`Syncer` des fiches
  `catalog-info.yaml`, `StackLoader` des stacks : repo du client et catalogue officiel,
  `stack.go` le format, `parameters.go` les paramètres et leur JSON Schema), `internal/webhook`,
  `internal/metrics`, `internal/database` (migrations embarquées).
- `frontend/src/visit/` : mode visite (GitHub Pages, `npm run build:visit`) : l'API servie
  par `fixtures.json`, capturé sur une démo (`demo/visit/capture-fixtures.sh`), lecture
  seule. À recapturer quand l'API change.
- `stacks/` : catalogue officiel, embarqué dans le binaire (`go:embed`) ; une stack par
  dossier, sans provider désigné, une CI par forge (`ci_templates`). Fichiers du
  squelette en `.tmpl` (créés sans ce suffixe) pour que le module Go ne les compile pas.
- `documentation/reference/` : configuration de référence (`symphony.yaml`, chaque clé),
  bootstrap et stack de référence (`stack.yaml`), vérifiés par les tests.
- `quickstart/` : Compose (Symphony, PostgreSQL, proxy Docker) branché sur une forge
  existante, configs GitLab et GitHub, avec les stacks officielles.
- `demo/` : `up.sh` (rejouable), `seed.sh` (projets de démo par l'API), `gitlab/` (contenu
  poussé dans le groupe `platform` de la GitLab de démo : config, stacks propres à la
  démo, copies des officielles avec leurs providers, repo de déploiement ; à tenir à jour
  avec `stacks/`), `keycloak/`, `k3s/`, `capture/`
  (GIF du README), `SCENARIO.md`.

## Règles qui évitent les régressions

**Sécurité**
- Toute route `/api/v1` déclare son rôle (`r.With(viewer|developer|admin)`) ; sinon un
  utilisateur connecté sans rôle y a accès. Utilisateur : `auth.UserFrom(ctx)`. Toute route
  est décrite dans `internal/api/openapi.yaml` (servie sur `/api/openapi.yaml`, avec son
  `x-symphony-role`) : `TestOpenAPICoversRoutes` échoue sinon.
- Un handler qui agit sur un projet (déployer, pipeline, action, logs…) appelle
  `authorizeProject` : admin, ou developer membre de l'équipe du projet (`projects.team`).
- Un handler qui modifie quelque chose écrit dans l'audit (`audit(db, r, …)`).
- Jamais de secret dans un fichier, un log, `x.Set` (renvoyé par l'API) ni le repo de
  déploiement. La config désigne ses secrets par `secret://<provider>/<chemin>#<clé>`
  (`secret://env/VAR` pour une variable d'environnement) ; les providers ne voient que la
  config résolue. Une référence introuvable fait refuser **toute** la configuration.
- Configuration stricte : `symphony.yaml` est décodé avec `KnownFields`, et chaque type de
  provider déclare ses clés dans `cmd/symphony/schema.go`. Nouveau type ou nouvelle clé :
  l'y déclarer, et la documenter dans `documentation/reference/symphony.yaml`
  (`TestReferenceCoversSchema` échoue sinon ; de même pour un champ de stack et
  `reference/stack.yaml`). Pas de repli silencieux (jeton par défaut, provider
  ignoré) : une erreur de construction fait refuser la configuration.
  `TestRepoConfigsValid` et `TestValidateRepo` valident les configs livrées.
- `resolveInputs` refuse les entrées d'action non déclarées (injection de variables CI) :
  ne pas assouplir. De même, les paramètres d'une stack passent par `Stack.Resolve`
  (JSON Schema, `additionalProperties: false`) avant d'être écrits dans le squelette.

**Providers**
- Le contenu de `provider.Registry` est remplacé d'un bloc au rechargement : ne jamais
  garder un provider dans une struct longue durée, le résoudre à chaque appel.
- Une méthode ajoutée à une interface l'est aussi dans `instrumented`. Chaque provider a
  un `Check()` en lecture seule (page Administration) : un nouveau provider en a un.
- Deux « registry » : `provider.Registry` (tous les providers) et `RegistryProvider` (images).
- Supprimer un projet passe par **chaque** runtime (`Remove`) puis le repo et les images
  (`DeleteImages`) : rien ne doit rester. GitLab ne fait que programmer la suppression d'un
  repo : `DeleteRepo` la force (`permanently_remove`).
- Runtime `argocd` : jamais d'appel à l'API Kubernetes, tout passe par Git ; `repo_url` est
  l'URL vue par Argo CD ; Argo CD répond 403 pour une Application absente. Le chart Helm
  ou la base Kustomize d'une stack (`spec.kubernetes` de la fiche, lu sur main au
  déploiement) est copié dans le dossier du service, avec `values.yaml` ou
  `kustomization.yaml` de Symphony (contrat dans `reference/stack.yaml`) ; `prune` retire
  les fichiers d'un déploiement précédent.
- Infra d'une stack (`spec.infra`, repris dans la fiche) : manifests d'opérateurs
  (CloudNativePG…) copiés dans `infra/` du dossier du service, nommés
  `<déploiement>-<nom>`, protégés de l'élagage (`Prune=false`), appliqués par la même
  Application : supprimer le projet les emporte. Runtime `argocd` seulement : la
  création est refusée si un environnement du projet tourne ailleurs
  (`checkInfraRuntimes`), et `docker` refuse un déploiement avec infra.
- GitHub : un workflow lancé par `workflow_dispatch` déclare toutes les entrées envoyées
  (`SYMPHONY_*` compris), sinon 422. Webhooks : `/webhooks/<plateforme>` ; une forge qui
  ne peut pas joindre Symphony (GitHub et une démo locale) laisse le sondage travailler.

**Environnements et secrets des applis**
- Un déploiement s'appelle `DeploymentName(projet, env)` et porte projet et environnement
  (labels Docker `symphony.project`/`symphony.environment`, Argo CD `symphony/project`/
  `symphony/environment`) : ne jamais retrouver un projet par le nom du déploiement.
- Environnement protégé : `Deploy` refuse, seul `Promote` y va (image saine de
  l'environnement précédent).
- Variables d'une appli : fiche (`spec.environments[].env`), jamais la demande. Secrets
  limités à `apps/<projet>/` (`checkAppSecret`), jamais `secret://env/…`. Un runtime qui
  écrit dans Git (`SecretRefs()` vrai) ne reçoit que des références ; les autres reçoivent
  des valeurs résolues au déploiement, jamais enregistrées.

**Tâches et API**
- Une étape de tâche peut être rejouée après un redémarrage : tester `x.Get` avant d'agir,
  `x.Set` juste après.
- Le front lit exactement les clés JSON : tags `json` en snake_case, et `frontend/src/types.ts`
  décrit chaque réponse (à mettre à jour avec les structs Go). Logique pure dans
  `src/lib/` (testée), composants dans `src/components/`, pages dans `src/views/`.
- Migration SQL : fichier `migrations/00N_*.sql`, sa variable `//go:embed` **et** la liste
  de `db.go`.

**Démo**
- La CI GitLab des stacks construit avec le socket du runner (démo) ou, sur
  gitlab.com ou avec `DOCKER_DIND=true`, un service `docker:dind` (jobs `build` /
  `build-dind`) : garder les deux chemins.
- Sans `GITHUB_TOKEN`, `up.sh` ne pousse pas les blocs `# github:début … fin` de la config
  ni les stacks `*-github` ; avec `--no-k8s`, de même pour `# k8s:début … fin` et
  les stacks `*-k8s` (et les blocs `# k8s:début … fin` des fiches de `seed/`). Vault est en
  mode dev (en mémoire) : `up.sh` le remplit à chaque fois. k3s a Argo CD, External
  Secrets et CloudNativePG (base d'`auth-service`, stack `api-rest-python-k8s`).

## Conventions

- Commentaires, logs, erreurs d'API, docs et commits **en français**.
- `gofmt` (vérifié par la CI).
- Mettre à jour ce fichier, `documentation/architecture.md`, `ROADMAP.md` et `CHANGELOG.md`
  avec le code.

## Règles de travail

- **Demander avant d'écrire dans la GitLab de l'utilisateur** (config, stacks, repos de
  services), avec un dry-run de ce qui sera poussé.
- Tester de bout en bout quand un changement touche les providers, la config, les
  stacks ou la démo, en plus des tests unitaires.
- Tout se fait sur `main` ; pousser seulement quand c'est demandé. Ne pas toucher à la
  branche `legacy` (archive).
- Une version = cases de la roadmap cochées, `CHANGELOG.md`, tag annoté `vX.Y` poussé (la CI
  publie l'image), puis `gh release create`.

## Tests

- Unitaires partout ; sur une vraie base avec `internal/database/dbtest` (sautés sans
  `SYMPHONY_TEST_DB=1`, la CI fournit la base). Providers simulés : `internal/provider/fake`
  (forge SCM+CI dont chaque push donne une pipeline réussie et une image, registry,
  runtimes). `internal/api/integration_test.go` joue le parcours complet par l'API ; les
  droits par équipe sont dans `handlers/authz_test.go` (`auth.ContextWithUser`).

## Tests de bout en bout

- Serveur de test : binaire du scratchpad sur `PORT=18080` (le 8080 peut être pris).
- Projets temporaires `symphony-*-e2e`, supprimés après (`DELETE /api/v1/projects/{nom}`).
- Modification temporaire d'un repo de config ou de stacks : restaurer l'original et
  vérifier qu'il est identique.
- Démo sur ports décalés : `GITLAB_PORT=18929 REGISTRY_PORT=15050 GITLAB_SSH_PORT=12224
  SYMPHONY_PORT=18090 KEYCLOAK_PORT=18180 VAULT_PORT=18200 K8S_HTTP_PORT=18081
  ARGOCD_PORT=18088 K8S_API_PORT=16443 SYMPHONY_IMAGE=symphony:local ./demo/up.sh`, puis
  `./demo/down.sh --purge` (~5,5 Go de RAM avec k3s). Jeton d'API : grant `password` du
  client `symphony` sur Keycloak (`bob`/`bob`) ; navigateur : Playwright de
  `demo/capture` (`playwright-core`, Chrome de `~/.cache/ms-playwright`).
