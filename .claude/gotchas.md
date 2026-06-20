# Gotchas — Symphony

Journal vivant des pièges déjà rencontrés ou identifiés sur ce projet
précis. Contrairement aux skills (génériques, réutilisables ailleurs),
ce fichier n'a de sens que pour Symphony — il capture ce qu'on a appris
en le regardant, pas une doctrine transférable.

Règle d'usage : avant de toucher une zone listée ici, lire l'entrée
correspondante. Après avoir corrigé un piège ou découvert un nouveau,
ajouter/mettre à jour une entrée — daté, avec le fichier concerné.

---

## Dépendances

### `go-git` présent mais non utilisé
**Depuis** : revue initiale du code (2026-06).
**Où** : `go.mod` (dépendance directe), absent de
`internal/providers/scm/gitlab/gitlab.go` (qui fait du HTTP brut vers
l'API REST GitLab).
**Piège** : ne pas supposer que `go-git` est utilisé quelque part dans
le projet sans vérifier — c'est probablement un résidu d'un essai
précédent. Ne pas s'appuyer dessus pour un nouveau besoin (ex: clone
local) sans confirmer d'abord s'il est encore d'actualité ou à
supprimer.
**Statut** : non résolu — à trancher (garder avec un usage identifié, ou
retirer du `go.mod`).

### `github.com/lib/pq` non maintenu
**Depuis** : revue initiale du code (2026-06). Confirmé par audit
architecture-guardian (2026-06-20).
**Où** : `internal/database/db.go` (`_ "github.com/lib/pq"`,
`sql.Open("postgres", ...)`), `go.mod`.
**Piège** : ce driver PostgreSQL n'est plus activement maintenu par son
auteur d'origine. **Correction par rapport à l'entrée d'origine** : ce
n'est pas une dépendance indirecte mais un **blank import direct et
actif** dans `db.go` — `go.mod` le liste à tort dans le bloc
`// indirect`, ce qui désynchronise le fichier de la réalité du code.
`pgx`/`pgx/v5` n'est importé nulle part dans le projet à ce stade.
**Statut** : non résolu — migration cible vers `pgx`/`pgx/v5` à faire
avant que la base de code grossisse (voir skill
`async-task-state-machine`, `references/schema-guidance.md`).

### Binaire et script tiers trackés dans le repo
**Depuis** : audit architecture-guardian (2026-06-20).
**Où** : racine du repo — `go1.22.5.linux-amd64.tar.gz` (~69 Mo),
`script.deb.sh`.
**Piège** : ces fichiers alourdissent inutilement le repo et créent une
incohérence de version (Go 1.22.5 dans l'arbre vs Go 1.25 visé par
`go.mod`/CLAUDE.md). Aucun `.gitignore` à la racine pour les exclure ni
empêcher leur réapparition.
**Statut** : résolu (2026-06-20, commits `c8f9665`/`1b025c8`) —
`go1.22.5.linux-amd64.tar.gz` scrubbé de tout l'historique git
(`git filter-branch` + force-push), `script.deb.sh` retiré du repo
(suppression simple, sans réécriture d'historique — jugé non sensible).
Reste un point séparé non couvert ici : aucune entrée `.gitignore`
n'empêche un nouveau gros binaire de revenir par erreur — pas de
`*.tar.gz`/`*.deb` ajouté au `.gitignore`.

---

## Driver GitLab (`internal/providers/scm/gitlab/gitlab.go`)

### Erreurs JSON ignorées silencieusement
**Depuis** : revue initiale du code (2026-06). Étendu par audit
architecture-guardian (2026-06-20).
**Où** : `api()` (erreur de `json.Marshal` ignorée), `CreateRepo` et
méthodes similaires (erreur de `json.Unmarshal(data, &project)`
ignorée). **Le pattern est plus répandu que prévu** : même type d'erreur
ignorée dans `internal/providers/ci/gitlabci/gitlabci.go` (lignes ~33,
90, 104) et `internal/providers/deploy/docker/docker.go` (lignes ~92,
122, 140).
**Piège** : si l'API GitLab/Docker renvoie un body inattendu (panne
partielle, changement de format), le code continue avec des valeurs zéro
sans jamais le signaler — un bug silencieux, pas un crash explicite.
**Ne pas reproduire** dans un nouveau driver — voir skill
`adapter-pattern`, `references/known-gaps-methodology.md` pour le détail
complet.
**Statut** : non résolu dans le code existant, sur les 3 drivers
concernés. Candidat à une petite PR de fiabilisation isolée couvrant les
3 fichiers ensemble plutôt que juste `gitlab.go`.

### Erreur de résolution de namespace avalée
**Depuis** : revue initiale du code (2026-06).
**Où** : `CreateRepo`, bloc `if req.Namespace != "" { if nsID, err :=
p.resolveNamespace(...); err == nil { ... } }`.
**Piège** : si la résolution du namespace échoue (typo, namespace
inexistant), le repo est créé quand même, mais sans le namespace
demandé — silencieusement. Un utilisateur qui demande explicitement un
namespace peut ne jamais s'apercevoir qu'il a atterri ailleurs.
**Statut** : non résolu — à corriger pour retourner une erreur explicite
si `req.Namespace != ""` et que la résolution échoue.

### Pas de pagination dans `ListRepos`
**Depuis** : revue initiale du code (2026-06). Étendu par audit
architecture-guardian (2026-06-20).
**Où** : `ListRepos`, `per_page=50` codé en dur, pas de suivi des pages
suivantes. **Même piège retrouvé** dans
`internal/templates/loader.go:92` (`per_page=50` sur l'appel
`repository/tree` qui charge le catalogue de golden paths).
**Piège** : au-delà de 50 dépôts accessibles par le token, des dépôts
existants ne remontent jamais, sans erreur visible. Côté `loader.go`,
c'est plus grave : le catalogue de golden paths proposé aux devs peut
être tronqué silencieusement au-delà de 50 entrées dans le repo de
templates.
**Statut** : non bloquant pour le MVP actuel (peu de projets/templates
gérés), mais à corriger avant un usage à plus grande échelle — traiter
les deux occurrences ensemble.

---

## Sécurité

### Secrets commités dans `.env`, aucun `.gitignore`
**Depuis** : audit architecture-guardian (2026-06-20). Sévérité
revue à la baisse (2026-06-20) après clarification utilisateur.
**Où** : `.env` à la racine du repo — **tracké par git** (confirmé via
`git ls-files`) ; aucun `.gitignore` n'existe à la racine du projet.
**Piège** : le fichier contient des credentials en clair :
`GITLAB_TOKEN`, `GITLAB_RUNNER_TOKEN`, `DB_PASSWORD=symphony123`, et
`SYMPHONY_TOKEN` qui est **identique à `GITLAB_TOKEN`** — donc aucune
séparation de scope, un seul token tout-puissant utilisé partout.
**Nuance (confirmée par l'utilisateur)** : `GITLAB_URL=
http://gitlab.local:8929` et `DB_HOST=localhost` montrent que ce sont
des tokens/mots de passe **temporaires sur des instances locales**, non
joignables depuis l'extérieur — exploitabilité réelle faible
aujourd'hui, pas un incident actif. Le risque n'est pas "ces credentials
précis sont dangereux maintenant", mais le **pattern** : sans
`.gitignore`, le prochain token réel/production suivant le même chemin
fuitera de la même façon, silencieusement.
**Statut** : volet hygiène git résolu (2026-06-20) — `.env` ajouté au
`.gitignore`, `git rm --cached .env` (untracké, conservé sur disque),
et tout l'historique git scrubbé via `git filter-branch` + force-push
(commit `7b9579c`) : `.env` n'existe plus dans aucun commit accessible,
y compris ceux contenant l'ancien `GITLAB_TOKEN`/`SYMPHONY_TOKEN`.
**Reste non résolu** : `GITLAB_RUNNER_TOKEN` et `DB_PASSWORD=symphony123`
sont toujours présents en clair dans le `.env` local (non commité
désormais, mais toujours en clair sur disque) — la rotation de ces deux
credentials et la fourniture d'un `.env.example` sans valeurs n'ont pas
été faites. Le fichier `.env` reste donc à traiter côté contenu, plus
seulement côté tracking git.

### Aucune authentification — OIDC absent malgré la règle "non négociable dès le MVP"
**Depuis** : audit architecture-guardian (2026-06-20).
**Où** : `internal/api/server.go` — middlewares présents : `Logger`,
`Recoverer`, `corsMiddleware` (qui met `Access-Control-Allow-Origin: *`).
Aucun import OIDC/OAuth/JWT trouvé dans le code (`server.go`,
`handlers.go`, `projects.go`, `main.go`).
**Piège** : toutes les routes, y compris les actions mutantes
(`POST /api/v1/projects`, `POST /api/v1/deployments`,
`POST /api/v1/templates/reload`), sont ouvertes sans authentification.
L'audit log écrit `user_id = 'system'` en dur sur tous les appels
(`internal/database/schema.go`), donc même l'audit trail ne distingue
pas les utilisateurs réels.
**Statut** : non résolu — CLAUDE.md classe l'auth OIDC comme non
négociable dès le MVP ; c'est une dette de sécurité bloquante, pas un
oubli neutre. Décider du point d'insertion du middleware OIDC avant
d'ajouter de nouvelles routes mutantes. **Aggravation confirmée par
audit security-baseline-reviewer (2026-06-20)** : combiné au
provisioning direct (décision #3), n'importe qui ayant un accès réseau
à l'API peut appeler `DELETE /api/v1/deployments/{id}` ou
`POST /api/v1/projects` pour détruire/créer des ressources réelles via
le token de service de Symphony — pas un risque théorique, exploitable
dès que le service sort du poste de dev.

### `DB_PASSWORD` codé en dur comme fallback silencieux
**Depuis** : audit security-baseline-reviewer (2026-06-20).
**Où** : `internal/database/db.go:23` —
`getEnv("DB_PASSWORD", "symphony123")`.
**Piège** : contrairement à `GITLAB_TOKEN` (`cmd/symphony/main.go`, qui
fait `log.Fatal` si absent), si `DB_PASSWORD` n'est pas défini en env,
Symphony se rabat silencieusement sur `symphony123` au lieu d'échouer.
Ce mot de passe par défaut vit dans le **binaire compilé** lui-même,
donc redistribuable avec l'image Docker. C'est la seule instance
concrète trouvée du pattern "secret en dur" interdit par CLAUDE.md.
**Statut** : non résolu — supprimer le fallback, faire `log.Fatal` si
`DB_PASSWORD` absent, même pattern que `GITLAB_TOKEN`. Corriger aussi
`sslmode=disable` (ligne 18) dès que la DB n'est plus en localhost.

### Token GitLab structurellement scopé instance, pas groupe
**Depuis** : audit security-baseline-reviewer (2026-06-20).
**Où** : `internal/providers/scm/gitlab/gitlab.go` (`GET
/projects?membership=true`), `internal/providers/ci/gitlabci/runner.go`
(`GET /runners?type=instance_type`, `gitlab-runner register` niveau
instance).
**Piège** : la règle CLAUDE.md "un token GitLab de Symphony n'a accès
qu'au groupe qu'il gère, jamais à l'instance entière" est aujourd'hui
**inviolable par construction** — ces appels exigent un token
admin-instance pour fonctionner, ce n'est pas juste le token actuel qui
est trop large, c'est le code qui réclame ce scope. Risque théorique
tant que l'instance est `gitlab.local`, mais bloquant pour tout
déploiement réel.
**Statut** : non résolu — refactorer vers des appels scopés groupe, et
faire échouer `CreateRepo` explicitement si `namespace_id` ne se résout
pas (au lieu de créer à la racine, cf. entrée "Erreur de résolution de
namespace avalée").

---

## Exécution applicative exécutée directement par Symphony (violation décision #3)

### Le `DeployProvider` Docker exécute le déploiement de façon synchrone et directe
**Depuis** : audit architecture-guardian (2026-06-20).
**Où** : `internal/providers/deploy/docker/docker.go` (`Deploy`,
`Stop`), appelé synchroniquement depuis
`internal/api/projects.go` (`deployProject`, ~ligne 105).
**Piège** : `Deploy()` fait `POST /containers/create` puis
`POST /containers/{id}/start` directement sur le socket Docker, dans le
handler HTTP, et renvoie immédiatement `status: "running"` persisté en
dur. C'est exactement le type d'« exécution applicative sur un système
vivant » que la décision d'architecture #3 (voir CLAUDE.md) interdit à
Symphony de faire lui-même — ça doit être délégué à un pipeline externe
puis observé de façon asynchrone (webhook/polling), comme c'est déjà le
cas pour les `pipelines` (voir `state-machine-conventions`).
**Conséquence directe** : la table `deployments` n'a pas de cycle de vie
`PENDING → RUNNING → SUCCESS/FAILED` comme la table `pipelines` — il n'y
a pas d'état intermédiaire à modéliser puisque l'exécution est
synchrone.
**Statut** : non résolu — c'est le finding architectural le plus
structurant de l'audit. À remonter explicitement et trancher le design
cible (déléguer le déploiement à un pipeline + observation async) avant
d'ajouter des features sur ce code. **Conséquence confirmée par audit
async-state-engineer (2026-06-20)** : si `s.deploy.Deploy()` retourne
une erreur, le handler répond 500 mais **n'insère aucune ligne** en
DB (`internal/api/projects.go`, l'`INSERT` est après le check d'erreur)
— une tentative de déploiement échouée ne laisse aucune trace, ni dans
`deployments` ni dans `audit_log`. Pire que le statut figé déjà
identifié : perte totale d'information sur les échecs.

### `EnsureRunner` shelle vers `docker run --privileged` avec le socket Docker hôte monté
**Depuis** : audit architecture-guardian (2026-06-20).
**Où** : `internal/providers/ci/gitlabci/runner.go` (`EnsureRunner`,
`startRunnerContainer`).
**Piège** : le code fait `exec.Command("docker", "run", "-d", ...,
"--docker-privileged", "-v", "/var/run/docker.sock:/var/run/docker.sock",
...)` puis `docker exec ... gitlab-runner register`. Trois problèmes
cumulés : (1) exécution applicative directe sur l'hôte — même violation
de la décision #3 que ci-dessus ; (2) un runner `--privileged` avec le
socket Docker monté permet un échappement de conteneur trivial,
l'opposé du "scope minimal / blast radius limité" exigé par la section
Sécurité de CLAUDE.md ; (3) shell-out vers le binaire `docker` externe
au lieu de passer par l'abstraction HTTP utilisée ailleurs dans le
projet — dépendance runtime cachée hors du contrat d'interface
(décision #2). Contient aussi un `time.Sleep(3 * time.Second)` bloquant
dans un chemin appelé au runtime (tension avec le principe stateless,
décision #4).
**Statut** : non résolu — à traiter avec l'entrée précédente, même
décision de design.

---

## Conformité au pattern driver (audit interface-driven-integrator, 2026-06-20)

### `DeployProvider` est taillé sur mesure pour Docker mono-instance — pas prêt pour Kubernetes
**Depuis** : audit interface-driven-integrator (2026-06-20).
**Où** : `internal/providers/interfaces.go:75-102` (`DeployRequest`,
`DeployResult`, `Status`), `internal/providers/deploy/docker/docker.go`.
**Piège** : `DeployRequest` n'a ni `Replicas`, ni `Namespace` — un futur
driver Kubernetes ne peut pas honorer "déployer une recette à N
instances" (golden path #6 du CLAUDE.md) sans étendre le contrat.
`Status()` retourne une `string` libre sans vocabulaire commun entre
drivers (Docker renvoie `result.State.Status` brut type `"running"`) —
un 2e `DeployProvider` inventera forcément ses propres valeurs, rendant
tout code appelant incapable de traiter les statuts de façon uniforme.
`DeployResult.URL` suppose une URL directement adressable, absente par
défaut pour un déploiement Kubernetes sans ingress.
**Statut** : non résolu — contredit la table CLAUDE.md qui prévoit
Kubernetes/Terraform "via la même interface" sans réécriture. À trancher
avec l'agent d'architecture avant d'écrire un driver Kubernetes : a
minima ajouter `Replicas`/`Namespace` et documenter un vocabulaire de
statuts communs.

### Le pipeline GitLab CI généré couple en dur le driver CI au driver SCM GitLab
**Depuis** : audit interface-driven-integrator (2026-06-20).
**Où** : `internal/providers/ci/gitlabci/templates.go:96` —
`git clone http://root:$SYMPHONY_TOKEN@gitlab.local:8929/root/symphony-config.git`.
**Piège** : le template de pipeline généré par le driver `CIProvider`
suppose que le driver `SCMProvider` actif est GitLab, à une URL fixe, et
qu'un repo `symphony-config` existe à ce chemin précis. Si SCM devient
GitHub un jour alors que CI reste GitLab CI (combinaison valide selon la
table CLAUDE.md), ce pipeline est cassé silencieusement. C'est un
couplage caché entre deux catégories de driver censées rester
indépendantes (décision #2).
**Statut** : non résolu — paramétrer l'URL du SCM et du repo de config
comme variables injectées dans le pipeline plutôt qu'en dur dans le
générateur de YAML.

### Aucun constructeur de driver ne valide ses paramètres — échec tardif, pas explicite
**Depuis** : audit interface-driven-integrator (2026-06-20).
**Où** : `gitlab.New`, `gitlabci.New`, `gitlabregistry.New`, `docker.New`
— les 4 constructeurs acceptent silencieusement une URL/token/socket
vide.
**Piège** : seul `GITLAB_TOKEN` est validé, et seulement dans
`cmd/symphony/main.go` (`log.Fatal` si vide) — avant l'appel à
`gitlab.New`, pas dans le constructeur lui-même. Rien ne valide
`REGISTRY_URL`, `DOCKER_SOCKET`, `GITLAB_URL`. Un admin qui mal
configure `DOCKER_SOCKET` (typo) ne le découvre qu'au premier `Deploy()`
en runtime, avec une erreur peu actionnable — contredit le persona admin
("échouer tôt et explicitement" attendu par le skill `adapter-pattern`).
**Statut** : non résolu — ajouter une validation/health-check à la
construction de chaque driver, pas seulement sur `GITLAB_TOKEN`.

### `DeleteImage` non implémentée dans le driver registry
**Depuis** : audit interface-driven-integrator (2026-06-20).
**Où** : `internal/providers/artifacts/gitlabregistry/registry.go:74-77`
— retourne `fmt.Errorf("not implemented")`.
**Piège** : conforme à la signature du contrat mais pas à sa substance —
un appelant ne peut jamais réussir un `DeleteImage` avec ce driver. Pas
un swallow silencieux (l'erreur est explicite, bon point), mais une
lacune connue à documenter plutôt que découvrir en prod.
**Statut** : non résolu — implémenter ou documenter explicitement comme
limitation du driver GitLab Registry.

---

## State machine asynchrone (audit async-state-engineer, 2026-06-20)

### Pas de garde de transition sur `UpdatePipelineStatus` — un poll tardif peut rouvrir une tâche terminée
**Depuis** : audit async-state-engineer (2026-06-20).
**Où** : `internal/database/pipelines.go:25-30` —
`UPDATE ... SET status=$1 WHERE pipeline_id=$2` sans lire l'état
courant avant écriture. `schema.go:25` : `status VARCHAR(50)` sans
contrainte `CHECK` ni enum.
**Piège** : n'importe quelle valeur de statut peut être écrite à
n'importe quel moment, y compris repasser d'un état final (`success`,
`failed`) vers un état intermédiaire — exactement le cas que le skill
`async-task-state-machine` interdit explicitement.
**Statut** : non résolu — `UPDATE ... WHERE pipeline_id=$2 AND status
NOT IN ('success','failed')`, ou lire l'état avant d'écrire.

### Pas de reconciliation au démarrage — tâches fantômes garanties après un redémarrage
**Depuis** : audit async-state-engineer (2026-06-20).
**Où** : `cmd/symphony/main.go` — aucun appel qui liste les pipelines
`running`/`pending` au boot pour les revérifier auprès de GitLab.
**Piège** : si le process redémarre pendant qu'un pipeline tourne côté
GitLab et que personne ne repolle ce pipeline précis depuis l'UI, son
statut reste bloqué en DB indéfiniment. Violation directe de la décision
#4 (stateless : le process doit pouvoir redémarrer sans rien perdre) —
la mémoire est bien en DB, mais rien ne s'en ressert pour rattraper la
réalité externe au redémarrage.
**Statut** : non résolu — au boot, lister les pipelines (et plus tard
déploiements) `pending`/`running` et revérifier leur statut réel avant
de servir du trafic.

### Transitions de statut via polling jamais auditées
**Depuis** : audit async-state-engineer (2026-06-20).
**Où** : `getPipelineStatusHandler`
(`internal/api/handlers.go:134-148`) appelle
`s.db.UpdatePipelineStatus(...)` mais jamais `s.db.Log(...)`.
**Piège** : la transition la plus significative de la state machine
pipeline (passage vers `success`/`failed`) est absente de l'audit trail,
alors que `triggerPipelineHandler` logue bien la création — incohérence
qui trahit un oubli plutôt qu'un choix délibéré.
**Statut** : non résolu — ajouter un appel `s.db.Log(...)` dans
`getPipelineStatusHandler` à chaque changement de statut effectif.

### Connexion DB sans retry ni configuration de pool
**Depuis** : audit async-state-engineer (2026-06-20).
**Où** : `internal/database/db.go:16-35` (`Connect`) — un seul
`db.Ping()`, pas de retry/backoff ; pas d'appel à `SetMaxOpenConns`,
`SetMaxIdleConns`, `SetConnMaxLifetime`.
**Piège** : si PostgreSQL n'est pas encore prêt au démarrage de
Symphony (cas fréquent en `docker-compose up`), `main.go` fait
`log.Fatalf` immédiatement sans retry — fragile pour un process censé
scaler horizontalement (décision #4). Sans limite de pool, plusieurs
instances de Symphony scalées peuvent saturer collectivement
PostgreSQL.
**Statut** : non résolu — ajouter un retry/backoff sur la connexion
initiale et configurer le pool explicitement.

---

## Contrat REST backend/frontend (audit rest-contract-reviewer, 2026-06-20)

### Trois conventions de casse JSON coexistent sans cohérence
**Depuis** : audit rest-contract-reviewer (2026-06-20).
**Où** : `internal/api/projects.go`/`handlers.go` (tags `json:` en
snake_case pour les requêtes entrantes), `internal/database/*.go` et
`internal/providers/interfaces.go` (aucun tag → PascalCase par défaut de
Go pour les entités lues), `internal/catalog/types.go` (tags forcés en
PascalCase explicite).
**Piège** : le frontend (`Projects.vue`, `Deployments.vue`) ne
"fonctionne" qu'en ayant été écrit en miroir exact de chaque
incohérence (`registry_url` ici, `p.Name`/`d.Status` là). La prochaine
route ajoutée sans vérifier le comportement par défaut de Go reproduira
le problème.
**Statut** : non résolu — choisir une seule convention (recommandé :
tags `json:` explicites partout, camelCase ou snake_case) et l'appliquer
à `database.*`, `providers.*` et `catalog.*` en miroir avec le frontend.

### Codes HTTP incohérents pour les échecs d'appel à un provider tiers
**Depuis** : audit rest-contract-reviewer (2026-06-20).
**Où** : `createProject` (échec `scm.CreateRepo`), `listRepos` (échec
`scm.ListRepos`), `triggerPipelineHandler` (échec `ci.TriggerPipeline`),
`deployProject` (échec `deploy.Deploy`) — tous renvoient **500**.
Seul `triggerAction` (échec webhook, `internal/api/handlers.go:71-75`)
renvoie correctement **502** pour un échec d'appel amont.
**Piège** : incohérence interne au même fichier — un échec GitLab/Docker
est traité comme une erreur interne Symphony partout sauf un endroit.
**Statut** : non résolu — généraliser le 502 à tout échec d'appel
provider tiers, garder 500 pour les vraies erreurs internes (DB).

### Validation `min`/`max` des inputs jamais appliquée dans `triggerAction`
**Depuis** : audit rest-contract-reviewer (2026-06-20).
**Où** : `internal/api/handlers.go:61-62` — `Decode(&inputs)` ignore
son erreur ; `catalog.Input.Min`/`Max` (`internal/catalog/types.go:50-54`)
existent mais ne sont vérifiés nulle part avant dispatch au webhook.
**Piège** : un input hors bornes ou un body malformé est transmis tel
quel au webhook externe sans 400 préalable — risque déjà identifié dans
le skill `api-contract-discipline` mais non traité dans le code actuel.
**Statut** : non résolu.

---

## Frontend (audit frontend-contract-developer, 2026-06-20)

### `baseURL` codé en dur + erreurs silencieuses = UI vide qui semble fonctionner
**Depuis** : audit frontend-contract-developer (2026-06-20).
**Où** : `frontend/src/api.js:3` —
`axios.create({ baseURL: 'http://localhost:8080' })`, figé au moment du
`vite build` dans le bundle embarqué. Aucun `VITE_API_URL`, aucun
`import.meta.env`, aucun mécanisme Go (`embed`/`StaticFS`) trouvé pour
servir le frontend buildé. Combiné à des `catch` vides dans
`Deployments.vue:46`, `Catalogue.vue:226`, `Projects.vue:238/247/256`.
**Piège** : c'est la combinaison la plus dangereuse de l'audit — un
admin qui déploie Symphony hors `localhost:8080` (donc presque tout
déploiement réel) voit toutes les requêtes échouer, et comme les erreurs
sont avalées silencieusement, l'UI affiche une liste vide identique à
un "rien à afficher" légitime. Aucun message n'explique que le frontend
ne parle à aucun backend.
**Statut** : non résolu — remplacer `baseURL` par une URL relative
(le frontend est censé être embarqué dans le même binaire que l'API) ou
`import.meta.env.VITE_API_URL`, et remplacer chaque `catch` vide par un
état d'erreur visible distinct de la liste vide.

### `Deployments.vue` ne poll pas — statut figé après l'action initiale
**Depuis** : audit frontend-contract-developer (2026-06-20).
**Où** : `frontend/src/views/Deployments.vue` — pas de `setInterval`
contrairement au pattern `pollStatus` déjà présent dans `Projects.vue`.
**Piège** : tant que `deployments` n'a pas de vrai cycle PENDING côté
backend (voir entrée Docker deploy), le frontend devrait au moins être
prêt à refléter un changement d'état sans action utilisateur — sinon ça
crée l'habitude UX d'un statut qu'on ne revérifie jamais.
**Statut** : non résolu.

---

## Configuration

### Incohérence de nommage `gitlab_ci` vs `gitlabci` — requalifié : `integrations.yaml` est en fait du code mort
**Depuis** : revue initiale du code (2026-06). **Requalifié** par audit
architecture-guardian (2026-06-20).
**Où** : `config/integrations.yaml` déclare `provider: gitlab_ci`, le
package réel est `internal/providers/ci/gitlabci/` (sans underscore).
`internal/providers/registry.go`, `cmd/symphony/main.go` (~lignes 40-43).
**Piège (vérifié, pire que supposé)** : `registry.go` ne fait **aucun
mapping** clé YAML → implémentation — il se contente d'un
`yaml.Unmarshal` dans une struct `IntegrationConfig`, et n'est même
jamais appelé par `main.go`. `main.go` instancie les 4 providers **en
dur** (`gitlabscm.New`, `gitlabci.New`, etc.) et ignore totalement
`integrations.yaml`. Donc le bug de résolution redouté n'existe pas
(rien ne résout cette clé), mais c'est conceptuellement pire : **toute
la config `integrations.yaml` est du code mort**, et la promesse "admin
configure en YAML sans recompiler" (décision #5 / persona admin) n'est
pas tenue aujourd'hui — changer de provider impose de recompiler.
**Statut** : non résolu — à concevoir : un registre/factory central
unique qui lit réellement `integrations.yaml` et résout vers
l'implémentation Go correspondante, avec le mapping de clés explicite à
un seul endroit (ce qui réglera aussi le risque de nommage d'origine).

---

## Zones désormais examinées (audit architecture-guardian, 2026-06-20)

Ces fichiers étaient listés comme "non vérifiés" — ils ont été lus en
détail. Résumé factuel, à ne plus supposer inconnu :

- **`internal/providers/registry.go`** : simple loader YAML
  (`yaml.Unmarshal`), jamais appelé par `main.go`, ne fait aucun mapping
  clé→driver malgré son nom. Voir entrée "Incohérence de nommage
  `gitlab_ci` vs `gitlabci`" ci-dessus.
- **`internal/templates/loader.go`** : **implémenté**, contrairement à
  ce que CLAUDE.md affirme ("pas encore implémenté", décision #5). Il
  charge golden paths + templates CI depuis un **repo GitLab distant**
  via l'API (pas des fichiers locaux), expose `Reload()`, et la route
  `POST /api/v1/templates/reload` est bien câblée
  (`server.go` → `handlers.go`). Drift à clarifier avec l'équipe : (a)
  le design "repo distant" vs "fichiers déclaratifs locaux" n'est pas
  ce que la doc laisse entendre ; (b) un hack mort `var _ =
  os.Getenv // garde l'import` (ligne ~184) à nettoyer ; (c) la route
  reload n'a aucune auth (voir entrée OIDC) ; (d) même piège de
  pagination `per_page=50` que `gitlab.go` (voir entrée correspondante).
  C'est une "Question ouverte" officielle de CLAUDE.md (#3) — le design
  exact reste à valider avec l'utilisateur, pas à supposer.
- **`internal/database/schema.go`** : 4 tables (`projects`,
  `pipelines`, `deployments`, `audit_log`), créées via un seul
  `db.Exec` idempotent (`CREATE TABLE IF NOT EXISTS`). Pas de
  versionnage de migrations, `user_id DEFAULT 'system'` partout (lié à
  l'absence d'OIDC), pas de contrainte enum sur les colonnes `status`.
- **`SCMProvider.Scaffold()` réel** (`scm/gitlab/scaffolding.go`) :
  pousse des fichiers de boilerplate (Dockerfile, main, tests,
  manifeste) générés **en dur dans le code Go** (`fmt.Sprintf` par
  langage : Go/Python/Node/Java). Cohérent côté provisioning (pousser
  des fichiers = OK, décision #3), mais contredit décision #5 : ajouter
  un langage de golden path = écrire du Go aujourd'hui, pas éditer un
  fichier. Piège de fiabilité supplémentaire : `Scaffold` logue les
  erreurs avec `fmt.Printf` et retourne toujours `nil` (lignes ~17-20)
  — un scaffold partiellement échoué est rapporté comme un succès.

Voir skill `declarative-scaffolding` pour la liste de questions à
clarifier avant d'aller plus loin dans cette zone — le design distant
de `loader.go` et le hardcoding de `scaffolding.go` sont deux signaux
qui vont dans le sens de cette prudence déjà documentée.

---

## Comment ajouter une entrée

```markdown
### Titre court du piège
**Depuis** : AAAA-MM (ou numéro de PR/commit si pertinent).
**Où** : fichier(s) concerné(s).
**Piège** : ce qui ne marche pas comme on l'attendrait, en une ou deux
phrases concrètes.
**Statut** : résolu (avec date/commit) ou non résolu (avec piste).
```

Garder chaque entrée courte et factuelle — ce fichier est un aide-mémoire
à relire vite avant de toucher une zone, pas un journal de bord détaillé.