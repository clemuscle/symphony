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
**Statut** : non résolu — à sortir du repo (et de l'historique si déjà
commités) et à exclure via `.gitignore`.

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

### Secrets réels commités dans `.env`, aucun `.gitignore`
**Depuis** : audit architecture-guardian (2026-06-20).
**Où** : `.env` à la racine du repo — **tracké par git** (confirmé via
`git ls-files`) ; aucun `.gitignore` n'existe à la racine du projet.
**Piège** : le fichier contient des credentials réels en clair :
`GITLAB_TOKEN`, `GITLAB_RUNNER_TOKEN`, `DB_PASSWORD=symphony123`, et
`SYMPHONY_TOKEN` qui est **identique à `GITLAB_TOKEN`** — donc aucune
séparation de scope, un seul token tout-puissant utilisé partout. Tout
token présent dans l'historique git doit être considéré comme
compromis, pas seulement "à ne pas réutiliser".
**Statut** : non résolu — à traiter en urgence : révoquer/régénérer les
3 tokens GitLab et le mot de passe DB, purger `.env` du suivi git et de
l'historique, ajouter un `.gitignore` racine, fournir un `.env.example`
sans valeurs réelles.

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
d'ajouter de nouvelles routes mutantes.

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
d'ajouter des features sur ce code.

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