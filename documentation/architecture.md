# Architecture de Symphony

Symphony est une **Internal Developer Platform** (IDP) : un backend Go et une SPA Vue 3 qui
s'appuient sur les outils existants (GitLab ou GitHub, leur CI, un registry d'images, un
runtime : Docker ou Kubernetes via Argo CD, un coffre de secrets, un fournisseur
d'identité) pour qu'un développeur crée un service prêt à déployer en un clic,
depuis une *stack* validée par l'équipe plateforme (un modèle de projet et son
formulaire borné).

Ce document décrit l'architecture actuelle et les décisions qui la fondent. Ce qui reste à
faire est dans [ROADMAP.md](../ROADMAP.md). Symphony est né d'une proposition d'architecture
appelée « KoreIDP » ; la section [Écarts avec KoreIDP](#écarts-avec-la-proposition-koreidp)
dit ce qui en a été repris, adapté ou écarté.

## Principes

1. **Tout se pilote par Git.** La configuration de Symphony, les stacks et la fiche
   catalogue de chaque projet vivent dans des repos Git, relus toutes les 15 s. Le binaire est
   immuable ; changer la plateforme, c'est pousser un commit.
2. **Des drivers, pas des plugins.** Chaque outil externe est derrière une interface Go
   stricte (`SCMProvider`, `CIProvider`, `RegistryProvider`, `RuntimeProvider`,
   `SecretsProvider`, `IdentityProvider`). Pas de chargement dynamique : ajouter un outil,
   c'est implémenter une interface. Chaque type de service a sa section de providers dans
   la configuration, avec plusieurs instances possibles.
3. **Déléguer l'exécution.** Symphony n'exécute pas les builds : il pousse un fichier CI et
   laisse la CI de la plateforme travailler. Sur Kubernetes, il ne déploie pas non plus : il
   commite des manifests, Argo CD les applique. Le runtime Docker local est l'exception
   assumée (voir les écarts).
4. **Aucun secret dans la configuration.** Les fichiers ne contiennent que des références :
   `secret://vault/symphony/gitlab#token` (lu dans Vault) ou `secret://env/GITLAB_TOKEN`
   (variable d'environnement).
5. **Une configuration stricte.** Chaque type de provider déclare ses clés : une faute de
   frappe, une clé requise absente ou un type inconnu font refuser la configuration,
   au lieu d'un réglage ignoré ou d'un repli silencieux sur une valeur par défaut.
6. **Rester léger.** Pas de SDK lourd (l'API Docker est appelée directement sur le socket),
   pas d'ORM, un seul binaire (front embarqué, image d'environ 15 Mo, 6 Mo compressée) et PostgreSQL.

## Vue d'ensemble

```
                    ┌──────────────────────────────┐
                    │  SPA Vue 3 (projets,         │
                    │  catalogue, déploiements)    │
                    └──────────────┬───────────────┘
                                   │ REST /api/v1
┌──────────────────────────────────▼───────────────────────────────────┐
│ Symphony (Go)                                                        │
│                                                                      │
│  api/ (chi, handlers)   gitops/Syncer      gitops/StackLoader        │
│  tasks/Runner ◄── webhooks GitLab          stacks (15 s) + officielles│
│  (tâches, SSE)          fiches (15 s)                                │
│  configWatcher (15 s)   catalogue.Store    database (PostgreSQL)     │
│                                                                      │
│  provider.Registry : SCM │ CI │ Registry │ Runtime │ Secrets │ Identity│
└──────┬────────────┬───────────┬───────────┬─────────┬───────┬──────┬─┘
       ▼            ▼           ▼           ▼         ▼       ▼      ▼
  GitLab /      GitLab CI /  Registry    Docker    Argo CD  Vault  Keycloak /
  GitHub        Actions      GitLab /    (socket)  ──► k8s  (KV)   OIDC
  (repos)                    GHCR
```

### Les repos Git

| Repo | Contenu | Relu |
|---|---|---|
| `platform/symphony-config` | `symphony.yaml` (providers, stacks, authentification…) | toutes les 15 s, appliqué à chaud |
| `platform/symphony-stacks` | `stacks/<nom>/` : `stack.yaml`, CI, `skeleton/` | toutes les 15 s |
| `apps/<service>` : un repo par projet | le code du service, généré depuis sa stack, et sa fiche `catalog-info.yaml` | la fiche, toutes les 15 s |
| `services/<service>` : un service existant | son code, sa CI et sa fiche, écrits sans Symphony (`catalogue.discovery`) | le groupe, toutes les 1 à 5 min ; la fiche, toutes les 15 s |
| `platform/symphony-deployments` | `apps/<service>/` : manifests Kubernetes écrits par Symphony (runtime `argocd`) | par Argo CD |

Trois groupes en démo : **platform**, tenu par l'équipe plateforme, **apps**, ouvert aux
développeurs (`providers.scm[].config.namespace` : groupe des repos créés), et
**services**, les services d'avant Symphony qu'il découvre. La fiche d'un
projet est dans **son** repo, pas dans la configuration de la plateforme : son équipe la
modifie par un commit, à côté du code qu'elle décrit (le modèle de `catalog-info.yaml`
chez Backstage). Le catalogue de Symphony, ce sont les stacks ; ses projets sont ce qui
en est sorti, et les services existants qu'il a découverts.

### Fiches Backstage et services existants

La fiche est un **Component Backstage** (`backstage.io/v1alpha1`) : une entreprise qui a
déjà ses fiches les garde. `gitops/fiche.go` la traduit dans le format interne
(`catalogue.Service`, celui que lit le front) : ce que Backstage a (`spec.owner`,
`lifecycle`, `type`, `dependsOn`, `metadata.links`, `tags`, `labels`) vient de ses champs ;
les valeurs simples de Symphony d'annotations `symphony.io/…` (tier, langage, port, contact
de l'équipe) ; les structures (environnements, actions, SLO, manifests, infra) de champs en
plus dans `spec`, que le schéma de Backstage accepte. Un fichier peut décrire plusieurs
entités : Symphony lit le Component du projet et ignore le reste (API, Resource…).
L'ancien format (`symphony/v1`, `kind: Service`) reste lu jusqu'à la v1.0, signalé dans la
page Administration ; Symphony écrit du Backstage pour les nouveaux projets.

La **découverte** (`gitops.Discoverer`, section `catalogue.discovery`) scanne des groupes
GitLab (sous-groupes compris) ou des organisations GitHub (`ListGroupRepos`, sans archives
ni forks) : chaque repo qui a une fiche devient un projet (`projects.discovered_from` =
`<scm>/<groupe>`), au nom de sa fiche, avec les providers CI et registry de sa source.
Symphony n'écrit rien dans ces repos : pas de webhook (les pipelines sont sondées), et
retirer le projet ne supprime que ses déploiements, jamais le repo ni les images. Un repo
qui n'est plus trouvé (fiche retirée, exclu, archivé) rend son projet **orphelin** : signalé,
pas retiré ; tant que sa source le donne encore, le retirer est refusé (409), le scan suivant
le reprendrait. Un nom déjà pris est un conflit signalé, jamais une fusion ; un groupe
illisible ne rend aucun projet orphelin.

Aux stacks du repo de l'entreprise s'ajoute le **catalogue officiel** (`stacks/` du repo de
Symphony), embarqué dans le binaire : versionné avec Symphony, lu sans réseau ni jeton, et
proposé seulement si la configuration l'expose (`stacks.official`, `["*"]` ou une liste).
Une stack officielle ne désigne aucun provider et porte une CI par forge
(`ci_templates`) : elle suit ceux par défaut de l'instance, GitLab ou GitHub. Une stack du
repo qui porte le même nom la remplace.

Le `symphony.yaml` local n'est qu'un **bootstrap** (section `config_source`) : comment
joindre GitLab et où lire la configuration. C'est le seul fichier qui demande un
redémarrage, comme l'`app-config` de Backstage.

### Installer Symphony

Trois façons : le [quickstart](../quickstart/README.md) (Symphony et PostgreSQL en
`docker compose`, branchés sur une forge existante), la démo complète, ou le chart Helm
`deploy/helm/symphony` pour un cluster Kubernetes. Chaque clé de configuration est
décrite dans [la configuration de référence](reference/symphony.yaml) (avec
[le bootstrap](reference/bootstrap.yaml) et [une stack](reference/stack.yaml)),
que la CI vérifie à chaque changement du schéma. Le chart crée un Deployment à une réplique (`Recreate` : une seule
instance exécute les tâches), un Service, un Ingress en option, et monte le bootstrap
depuis une ConfigMap (son empreinte relance le pod quand il change). Les secrets restent
dans un Secret existant chargé dans l'environnement, que la configuration référence par
`secret://env/…`. Le pod tourne en non-root, avec un système de fichiers en lecture seule,
toutes les capacités retirées et sans jeton de compte de service : Symphony n'appelle
jamais l'API Kubernetes (Argo CD applique). Le runtime `docker` n'a pas de sens dans un
cluster ; on y utilise `argocd`.

## Composants

| Paquet | Rôle |
|---|---|
| `cmd/symphony` | Composition : bootstrap et source de config (`config.go`), secrets et résolution des références (`secrets.go`), construction des providers (`providers.go`), schéma des providers (`schema.go`), `symphony validate` (`validate.go`), assemblage (`main.go`) |
| `internal/config` | Types de `symphony.yaml` et décodage (`${VAR}` remplacées par l'environnement) ; `Status`, l'état de la configuration en place pour la page Administration |
| `internal/provider` | Interfaces, `Registry` (providers par id, remplaçable d'un bloc), références `secret://` (`secretref.go`) et implémentations : `scm/gitlab`, `scm/github`, `ci/gitlabci`, `ci/githubactions`, `registry/gitlabregistry`, `registry/ghcr`, `runtime/docker`, `runtime/argocd`, `secrets/env`, `secrets/vault`, `identity/oidc`, `identity/keycloak` |
| `internal/gitops` | `Syncer` (fiche `catalog-info.yaml` de chaque projet, format dans `fiche.go`), `Discoverer` (services existants) et `StackLoader` (stacks du repo et catalogue officiel : format dans `stack.go`, paramètres et JSON Schema dans `parameters.go`) |
| `stacks` | Catalogue officiel de stacks, embarqué (`go:embed`) |
| `internal/catalogue` | Store en mémoire des fiches, dans le format interne (`catalogue.Service`) |
| `internal/tasks` | Moteur de tâches : étapes enregistrées en base, attente de pipelines (webhook ou sondage), reprise au démarrage, diffusion des changements (`Hub`, pour le SSE) |
| `internal/webhook` | Réception des événements de pipeline GitLab (`/webhooks/gitlab`, secret partagé) |
| `internal/database` | PostgreSQL via `database/sql` : `projects`, `pipelines`, `deployments`, `tasks`, `audit_log` ; migrations embarquées |
| `internal/api` | Routeur chi, handlers par domaine, `/healthz` |
| `internal/metrics` | Métriques Prometheus sur `/metrics` ; `internal/provider/instrumented` enveloppe chaque provider pour mesurer ses appels et retient le dernier (page Administration) |
| `internal/auth` | OIDC (connexion, retour, déconnexion), cookie de session signé, rôles, CSRF, CORS ; `Holder` remplacé d'un bloc au rechargement de la config |
| `internal/web` | Front compilé embarqué (`go:embed`), avec repli sur `index.html` pour les routes Vue |
| `frontend/` | Vue 3 + Vite : pages Projets (avec leur fiche), Catalogue (stacks et leur formulaire), Déploiements, Activité, Administration (admin) |

## Flux principaux

### Tâches asynchrones

Créer un projet, déployer, supprimer et lancer une action Day-2 prennent de quelques
secondes à plusieurs minutes (pipeline, purge du registry, synchronisation Argo CD).
L'API valide la demande, enregistre une **tâche** et répond `202` avec elle ; le moteur
(`internal/tasks`) l'exécute en arrière-plan.

```
PENDING ──► RUNNING ──► SUCCESS
               │   ▲
               │   └── pipeline terminée (webhook GitLab, ou sondage du provider CI)
               ▼
            FAILED
```

- Une tâche est une suite d'**étapes**. L'étape en cours et les résultats intermédiaires
  (`Exec.Set` : repo créé, pipeline lancée…) sont écrits en base à chaque pas. Au
  démarrage, Symphony relance les tâches non terminées à leur étape ; une étape rejouée
  saute ce que ses résultats disent déjà fait (pas de second repo).
- Une étape peut **attendre une pipeline** sans occuper de goroutine : la tâche est mise
  en attente (`wait` en base). Le webhook de GitLab la fait avancer à chaque changement
  d'état ; un sondage toutes les 15 s rattrape un webhook perdu et suffit seul pour une CI
  sans webhook. Au-delà d'une heure, la tâche échoue.
- Une seule tâche à la fois par projet ou service (`409` sinon).
- **Supprimer un projet** efface tout ce que Symphony a créé pour lui, dans l'ordre :
  ses déploiements sur chaque runtime (`Remove` : conteneurs et images locales pour
  Docker ; Application Argo CD supprimée en cascade puis manifests retirés du repo de
  déploiement), en attendant qu'ils aient disparu, puis le repo et ses images (purge du
  registry, puis suppression définitive : GitLab 18 ne fait sinon que la programmer),
  la fiche catalogue et les lignes en base. Chaque étape est rejouable. Une Application
  Argo CD du même nom que Symphony n'a pas créée est laissée en place.
- Chaque changement est diffusé en **Server-Sent Events** (`/api/v1/tasks/events`) :
  l'UI suit l'avancement sans recharger ni sonder. À la reconnexion, le front relit la
  liste : une coupure ne fait rien manquer.
- Les paramètres d'une tâche (qui peuvent contenir des variables d'environnement) ne
  sortent jamais de l'API ; les résultats ne contiennent pas de secret. Les identifiants
  du registry sont lus au moment du déploiement, jamais enregistrés.

### Créer un projet depuis une stack

Tâche `create_project`, après validation (nom DNS, stack, paramètres, compatibilité
SCM/CI, projet inexistant) :

0. **Paramètres** : la stack décrit son formulaire (`spec.parameters` : `name`, `title`,
   `type` string/number/boolean/choice, `choices`, `default`, `required`, `when`,
   `locked`), dans un format qu'un admin écrit sans connaître JSON Schema. Symphony le
   traduit en JSON Schema (draft 2020-12, servi par `GET /api/v1/stacks`) : `when`
   devient un `if/then/else` (sans objet, le paramètre est refusé), `locked` un `const`,
   et `additionalProperties: false` refuse un paramètre inconnu (comme `resolveInputs`
   pour les actions). `Stack.Resolve` complète les défauts puis valide avec ce schéma
   (`santhosh-tekuri/jsonschema`) ; ses erreurs sont traduites en français, un
   paramètre par ligne. Les valeurs sont gardées avec le projet (`stack_parameters`).
1. **Repo et squelette** : lire le skeleton et la CI **avant** de créer le repo (une
   stack cassée ne laisse pas de repo vide), retirer les fichiers dont la condition
   n'est pas remplie (`spec.files[].when`) et le suffixe `.tmpl`, remplacer les
   variables (`{{SERVICE_NAME}}`… et `{{params.<nom>}}`), créer le repo, puis pousser
   skeleton, CI et fiche `catalog-info.yaml` en **un seul commit**, qui lance la
   première pipeline. La fiche part de celle du skeleton s'il en a une (liens, SLO,
   actions de la stack), et reçoit l'équipe, le tier, le cycle de vie saisis, la stack
   (`metadata.labels.stack`) et ses manifests Kubernetes (`spec.kubernetes`) s'ils sont
   dans le repo.
2. **Projet et webhook** : enregistrer le projet (le `Syncer` lit alors sa fiche), abonner
   Symphony aux événements de pipeline du repo.
3. **Première pipeline** : attendre sa fin. Quand la tâche réussit, l'image du service est
   dans le registry : le projet est prêt à déployer.

Sur GitHub (stack `api-rest-go-github` de la démo, ou une stack officielle sur une
instance GitHub) : le repo est créé initialisé, puis le
squelette part en un commit par l'API Git (arbre, commit, branche), car l'API « contents »
ferait un commit (et un workflow) par fichier. Le workflow construit l'image et la pousse
sur GHCR (`ghcr.io/<owner>/<repo>`, provider `ghcr` : tags et identifiants de pull par
l'API des packages). Il déclare les entrées `SYMPHONY_*` de `workflow_dispatch` : GitHub
refuse une entrée non déclarée. Les statuts de GitHub Actions sont ramenés à ceux de
Symphony (`failure` → `failed`…) et l'id du run lancé est retrouvé (réponse de l'API ou
liste des runs). Le repo est abonné aux événements `workflow_run` (`/webhooks/github`, corps signé en
HMAC-SHA256 avec le secret des webhooks) ; le sondage reste en secours. À la
suppression du projet, le package GHCR est supprimé aussi (`DeleteImages`), ce que la
suppression du repo ne fait pas.

### Recharger la configuration à chaud

Le `configWatcher` relit `symphony.yaml` dans Git. Si le contenu a changé et qu'il est
valide, un nouveau `Registry` est construit puis substitué d'un bloc
(`Registry.Replace`) : les handlers gardent le même pointeur et voient soit l'ancien jeu de
providers, soit le nouveau. Le syncer et le chargeur de stacks sont repointés
(`SetSource`) et rechargés. Une configuration illisible ou invalide est refusée et la
précédente reste en place.

Le watcher garde l'état de la configuration (`config.Status`) : source, empreinte de la
version en place, dernière version refusée et sa raison, erreur de lecture ou de
secrets, et ce qui est appliqué mais à corriger (`warnings` : ancien nom de section). Le
chargeur de stacks et le syncer gardent le leur (stacks refusées et pourquoi, ancien
format, stack officielle remplacée, fiches illisibles, projets sans fiche). La page **Administration**
(`GET /api/v1/admin/status`, admin) les réunit, avec chaque provider et son dernier
appel ; `GET /api/v1/admin/providers/check` vérifie chaque provider en parallèle par sa
méthode `Check` (un appel en lecture : l'utilisateur du jeton, la version du daemon, la
découverte OIDC…), 20 s au plus chacun. Un admin lit ainsi pourquoi une stack
n'apparaît pas sans ouvrir les logs.

### Valider avant de pousser (`symphony validate`)

`symphony validate -config symphony.yaml -stacks stacks/` refait hors ligne ce que
fait le chargement : décodage strict, schéma de chaque provider, références de secrets
(forme et provider déclaré), construction des providers, environnements, `auth`,
webhooks, stacks officielles exposées, puis chaque stack du dossier (format,
paramètres, fichiers) contre ces providers. Les secrets ne sont
pas lus : chaque provider de secrets est remplacé par un substitut (sa propre
définition est vérifiée), et une variable `${VAR}` absente est laissée telle quelle et
signalée. Code de sortie 1 s'il y a une erreur, toutes rapportées d'un coup. L'image
étant `scratch` (sans shell), la CI d'un repo de configuration l'appelle par
`go run github.com/clemuscle/symphony/cmd/symphony@vX.Y validate …` (GitLab CI) ou
`docker run … ghcr.io/clemuscle/symphony:X.Y validate …` (GitHub Actions).

### Secrets (providers `secrets`)

Les providers de secrets sont construits **avant** les autres : `env` (variables
d'environnement) existe toujours, les autres (`vault`) peuvent prendre leurs propres
identifiants dans `env` ou dans un provider déclaré avant eux. Puis chaque valeur
`secret://<provider>/<chemin>#<clé>` de la configuration (providers, webhooks) est
remplacée par le secret, dans une **copie** : la configuration chargée ne contient jamais
de secret en clair. Une référence introuvable fait refuser toute la configuration :
appliquer une configuration à moitié résolue retirerait des providers qui marchaient.

À chaque cycle (15 s), même si le fichier n'a pas changé, les références sont relues avec
les providers de secrets en place ; si l'empreinte de la configuration résolue change
(rotation d'un jeton dans Vault), elle est réappliquée, sans redémarrage. Le bootstrap
local peut déclarer ses propres `providers.secrets` : le jeton qui lit la configuration
dans Git vient alors de Vault, lui aussi.

Le provider `vault` lit le moteur KV v2 (ou v1) et s'authentifie par jeton ou par
**AppRole** (`role_id`, `secret_id`), en renouvelant son jeton à expiration ou s'il est
révoqué. En démo, Symphony n'a dans son environnement que son AppRole, en lecture seule sur
`secret/symphony/*` ; `up.sh` y écrit les jetons GitLab, Argo CD et GitHub, le secret des
webhooks et celui du client de l'annuaire.

### Déployer

Tâche `deploy` : le runtime déploie, puis la tâche attend que le service tourne (Argo CD
synchronisé et pods prêts, ou conteneur démarré), 5 min au plus.

1. L'UI préremplit l'image et les tags depuis le registry du projet.
2. Le runtime Docker **tire l'image d'abord** (avec l'auth du registry, envoyée seulement
   si l'image y est hébergée) : si le pull échoue, l'ancien conteneur continue de tourner.
3. Bascule avec rollback : le nouveau conteneur démarre à côté (`<nom>-next`), l'ancien est
   arrêté. Si l'image a un `HEALTHCHECK`, Symphony attend l'état `healthy`
   (`health_timeout`, 2 min par défaut) ; sinon, le nouveau conteneur doit tourner encore
   après 3 s. S'il est prêt, il remplace l'ancien ; sinon (`unhealthy`, arrêté, délai
   dépassé) l'ancien redémarre et l'erreur contient les derniers logs du nouveau et la
   sortie de son healthcheck.

### Environnements et promotion

L'équipe plateforme déclare les environnements dans `symphony.yaml`, dans l'ordre de
promotion (`environments: [{name, runtime, protected}]`) ; une stack peut changer le
runtime d'un environnement (`spec.environments`, ex. `staging: argocd-k3s`). Un
déploiement vise un environnement : il s'appelle `<projet>-<environnement>` (conteneur,
Application Argo CD, hôte de l'Ingress) et porte le projet et l'environnement en labels,
ce qui permet de tout retrouver et de tout supprimer avec le projet.

Un environnement **protégé** (la production) refuse le déploiement direct : on y
**promeut** (`POST /api/v1/projects/{nom}/promote`) l'image exacte qui tourne, saine, dans
l'environnement précédent, éventuellement d'un runtime à l'autre (Docker en staging,
Kubernetes en production). Sans section `environments`, un projet a un seul déploiement,
comme avant.

### Variables et secrets des applications

Les variables d'une appli sont déclarées par environnement dans sa fiche, dans Git
(`spec.environments[].env`) : la demande de déploiement n'en porte pas. Une valeur peut
être une référence `secret://vault/apps/<projet>/<chemin>#<clé>`. Un projet ne lit que
ses secrets (`apps/<projet>/…`), jamais le provider `env` (l'environnement de Symphony,
avec ses jetons) ni les secrets d'un autre projet. Le runtime décide du transport
(`SecretRefs()`) :

- **Docker** : Symphony lit le secret au moment du déploiement et le passe au conteneur ;
  la valeur n'est jamais enregistrée (ni dans la tâche, ni en base).
- **Argo CD** : la valeur ne doit jamais aller dans Git. Le runtime écrit une
  `ExternalSecret` (références seules) ; External Secrets Operator, dans le cluster, lit
  Vault par le `ClusterSecretStore` configuré (`secret_store`) et crée le Secret
  `<déploiement>-env`, que le Deployment lit (`secretKeyRef`). Sans `secret_store`, un
  déploiement avec des secrets est refusé plutôt que d'écrire une valeur dans Git.

### Déployer sur Kubernetes (runtime `argocd`, GitOps)

Le runtime d'un projet vient de sa stack (`spec.providers.runtime`) ; le formulaire
de déploiement peut le changer. Avec `argocd`, Symphony ne touche jamais au cluster :

1. Il écrit les manifests du service dans le repo de déploiement, en **un commit** :
   `apps/<service>/deployment.yaml`, `service.yaml`, `ingress.yaml` et
   `kustomization.yaml`, qui porte ce qui change d'un déploiement à l'autre (tag de
   l'image, réplicas, annotation de redémarrage). L'historique Git est l'historique des
   déploiements ; un retour arrière est un `git revert`.
2. Il crée ou met à jour l'`Application` Argo CD du service (API REST d'Argo CD, `upsert`) :
   source = ce dossier, synchronisation automatique avec `prune` et `selfHeal`.
3. Il demande un rafraîchissement à Argo CD, qui applique le commit en quelques secondes
   au lieu d'attendre sa scrutation (3 min).
4. L'état affiché (Synced/OutOfSync, Healthy/Progressing/Degraded) est celui de
   l'`Application`. Arrêter un service le passe à 0 réplica, le relancer change
   l'annotation de redémarrage : toujours par un commit. Les journaux passent par l'API
   d'Argo CD.

**Manifests fournis par la stack.** Une stack peut livrer un chart Helm ou une base
Kustomize dans le repo créé (`spec.kubernetes`, repris dans la fiche) : l'équipe du
service les modifie comme son code. Au déploiement, Symphony les lit sur `main` et les
copie dans le dossier du service, puis écrit ce qu'il décide selon un contrat
documenté dans [la stack de référence](reference/stack.yaml) :

- **Helm** : `chart/` et, à côté, `values.yaml` (`fullnameOverride`, `replicaCount`,
  `image`, `containerPort`, `env`, `secretEnv`, `ingress`, `podAnnotations`) ;
  l'Application lit `chart/` avec ce fichier de valeurs. L'ExternalSecret des secrets est
  ajoutée aux templates du chart, en chaîne brute (Helm ne l'interprète pas).
- **Kustomize** : `base/` et un `kustomization.yaml` qui préfixe les noms par celui du
  déploiement, fixe l'image `app`, les réplicas, les variables (`configMapGenerator` et
  `envFrom` du premier conteneur), et ajoute l'Ingress et l'ExternalSecret.

Arrêter et relancer modifient `values.yaml` ou `kustomization.yaml` de la même façon. Les
fichiers d'un déploiement précédent qui ne servent plus (passage des manifests générés à
ceux de la stack, fichier retiré du chart) sont supprimés du repo de déploiement.

### L'infra avec l'appli

Une stack peut porter l'infra de son service (`spec.infra`) : des **manifests
d'opérateurs Kubernetes** dans un dossier du repo créé (un `Cluster` CloudNativePG pour
une base PostgreSQL, par exemple), et les variables que l'appli lit dans les secrets que
cette infra crée (`DATABASE_URL` ← clé `uri` du secret `db-app`). Elle est reprise dans
la fiche, que l'équipe peut modifier.

- **Une infra par environnement, en GitOps.** Au déploiement, le runtime `argocd` copie
  le dossier dans `infra/src/` du dossier du service et écrit `infra/kustomization.yaml` :
  objets nommés `<déploiement>-<nom>` (`payment-api-staging-db`), étiquetés, et annotés
  `Prune=false`. La même Application les applique avec le service ; Argo CD sait lire la
  santé d'un `Cluster` CloudNativePG, montrée sur la page du projet (`infra` de chaque
  déploiement, d'après les types relevés dans les manifests et notés sur l'Application).
- **Aucun identifiant chez Symphony.** L'opérateur crée la base et son secret dans le
  cluster ; le Deployment le lit (`secretKeyRef`). Ni le mot de passe ni l'URI ne passent
  par Symphony ou Git.
- **Pas de perte par erreur.** Un objet de l'infra retiré de Git n'est pas élagué : il
  reste dans le cluster (Argo CD le montre « à élaguer »). Seule la suppression du projet
  (Applications supprimées en cascade) emporte l'infra, volumes compris.
- **Kubernetes seulement.** Une stack dont l'infra serait créée est refusée à la création
  si un environnement du projet tourne sur un autre runtime ; le runtime `docker` refuse
  un déploiement avec infra. Avec un chart Helm, l'infra est dans le chart.

L'infra cloud (OpenTofu, appliqué par la CI du repo en plan puis apply, avec les
identifiants de la CI) viendra avec les premières cibles cloud (AWS, Scaleway, OVHcloud).
La CI teste la chaîne sur kind avec l'opérateur (`TestCNPGOnCluster` : base créée, jointe
avec l'URI de son secret, supprimée avec ses volumes).

Les identifiants du registry ne sont jamais écrits dans Git : le cluster tire les images
avec les siens (en démo, `registries.yaml` de k3s avec un jeton `read_registry`). Le
compte Argo CD de Symphony n'a droit qu'aux Applications du projet `default` et à leurs
journaux.

### Se connecter (OIDC, Backend-for-Frontend)

La connexion passe par un provider `identity` : `oidc` (n'importe quel fournisseur OIDC) ou
`keycloak` (issuer et découverte déduits de l'URL et du realm). La section `auth` ne garde
que la politique : quel provider (`auth.identity`) et quels groupes donnent quels rôles
(`auth.roles`).

1. Le front appelle `/api/v1/me` ; sans session, l'API répond 401 et le front envoie le
   navigateur sur `/auth/login?redirect=<page>`.
2. Symphony génère `state`, `nonce` et un vérificateur PKCE, les garde dans un cookie
   signé de 10 min, et redirige vers le fournisseur d'identité.
3. Au retour (`/auth/callback`), il vérifie `state`, échange le code contre les jetons
   (avec le vérificateur PKCE, de serveur à serveur), vérifie l'ID token (signature,
   `iss`, `aud`, `nonce`) et pose la session : identité et groupes, signés, 8 h. Aucun
   jeton n'est donné au navigateur ; l'ID token n'est gardé que dans un cookie limité à
   `/auth/logout`, pour une déconnexion sans écran de confirmation.
4. À chaque requête, le rôle est recalculé depuis les groupes et la configuration : un
   changement de `auth.roles` s'applique sans reconnexion.

En démo, GitLab se connecte au même realm Keycloak (OmniAuth OpenID Connect, client
`gitlab`) : un seul annuaire pour les deux outils. `up.sh` crée les comptes dans GitLab
avec leur e-mail (liés à Keycloak à la première connexion) et leur donne leurs droits :
alice Owner de `platform`, `apps` et `services`, bob Developer et carol Reporter de
`apps` et `services`. GitLab CE ne synchronise pas les groupes de l'annuaire (fonction
payante) : ces droits sont posés par le script, pas déduits des groupes Keycloak.

### Équipes et droits par projet

Un projet appartient à une **équipe** : un groupe de l'annuaire qui ne donne pas de rôle
(les groupes de `auth.roles` sont des rôles, les autres des équipes ; `GET /api/v1/me`
renvoie les deux). L'équipe est choisie à la création (un developer, parmi les siennes ;
un admin, n'importe laquelle), enregistrée en base (`projects.team`) et reprise comme
`owner` dans la fiche. Déployer, arrêter, relancer, lire les logs, relancer la pipeline et
lancer une action d'un projet demandent d'être admin, ou developer **membre de son
équipe** (`authorizeProject`) ; un projet sans équipe (antérieur à la v0.8) est réservé
aux admins. La lecture reste ouverte à tous les viewers. La base fait foi pour les droits :
changer l'`owner` de la fiche d'un projet créé par Symphony ne donne aucun droit. Un projet
**découvert**, lui, prend l'équipe de son `spec.owner` à chaque scan (sa fiche est la seule
source) ; une équipe que l'annuaire ne connaît pas (ou un groupe de rôle) le réserve aux
admins, et l'erreur se lit dans Administration.

`POST /api/v1/pipelines/trigger` prend un nom de projet et relance sa pipeline, sur son
repo et avec son CI, sans variables : une pipeline paramétrée passe par une action Day-2,
dont les entrées sont validées.

### Annuaire

Un provider `identity` peut avoir un annuaire (`ListGroups`). `keycloak` le lit par l'API
d'administration, avec un client de service (`client_credentials`, rôles `query-groups`
et `view-users` de `realm-management`), groupes mis en cache 30 s. `GET
/api/v1/identity/groups` les renvoie (`supported: false` sans annuaire) et le formulaire
de création propose les équipes comme owner du projet ; la saisie reste libre.

### Actions Day-2

Une fiche de catalogue peut déclarer des actions (« Relancer la pipeline »…) : un appel
webhook, ou une pipeline avec des entrées typées. Les entrées sont validées (bornes, options,
requis) et **les clés non déclarées sont refusées**, pour qu'un appelant ne puisse pas
injecter de variables CI arbitraires. Une action pipeline tourne sur le repo du projet
connu de Symphony, jamais sur celui qu'écrit la fiche (`spec.repo`, affiché seulement) :
un repo ne peut pas lancer les pipelines d'un autre avec le jeton de Symphony.

## Observabilité

`/metrics` (format Prometheus) expose :

| Métrique | Labels | Sert à voir |
|---|---|---|
| `symphony_http_requests_total`, `symphony_http_request_duration_seconds` | `method`, `route` (motif chi), `status` | trafic, latence et erreurs de l'API |
| `symphony_provider_calls_total`, `symphony_provider_call_duration_seconds` | `kind`, `provider`, `method`, `result` | santé et lenteur de GitLab, GitHub, Docker… |
| `symphony_reloads_total`, `symphony_last_successful_reload_timestamp_seconds` | `source` (catalogue, stacks, config), `result` (ok, error, refused) | un repo illisible, une config refusée, une synchro arrêtée |
| `symphony_catalogue_services`, `symphony_stacks` | — | ce qui est chargé |
| `symphony_tasks_finished_total`, `symphony_task_duration_seconds` | `kind`, `status` | tâches réussies ou échouées, et leur durée (pipeline comprise) |
| `symphony_webhooks_total` | `source`, `event`, `result` (ok, refused, ignored, error) | webhooks reçus, secrets invalides |

Plus les métriques du runtime Go et du processus. Les routes sont étiquetées par leur motif
(`/api/v1/projects/{name}`), jamais par le chemin, pour garder un nombre de séries borné.
Les appels aux providers sont mesurés par des décorateurs posés dans `buildProviders` : les
implémentations n'en savent rien.

## Sécurité

| Sujet | Aujourd'hui | Prévu |
|---|---|---|
| Authentification | OIDC (Keycloak en démo), Authorization Code + PKCE fait par le serveur ; cookie de session signé (`HttpOnly`, `SameSite=Lax`) ou jeton `Bearer` | — |
| Autorisations | rôles `viewer` < `developer` < `admin` déduits des groupes OIDC (configuration), vérifiés par route ; agir sur un projet (déployer, pipeline, actions) est réservé aux developers de son équipe et aux admins | — |
| CSRF | requête modifiante avec cookie : `Origin` (ou `Sec-Fetch-Site`) doit être Symphony | — |
| CORS | aucune origine tierce par défaut ; liste `cors_origins`, sans cookies | — |
| Secrets | providers de secrets (Vault par AppRole en démo, variables d'environnement) ; références `secret://` dans la configuration, résolues dans une copie ; jamais dans les fichiers, les logs, les résultats de tâches ni le repo de déploiement. Secrets des applis : limités à `apps/<projet>/`, passés par External Secrets sur Kubernetes | — |
| Production | environnement protégé : seule une image saine en staging y est promue | — |
| Argo CD | compte de service à jeton (`apiKey`), limité aux Applications du projet `default` et à leurs journaux | — |
| Runtime `docker` | créer un conteneur sur un daemon Docker équivaut à un accès root à la machine : runtime réservé à un poste ou à une démo (en production, `argocd`). Image non-root ; en démo, pas de socket monté dans Symphony mais un proxy qui n'ouvre que les conteneurs et les images | — |
| Fuite de token | l'auth du registry n'est envoyée qu'au registry qui héberge l'image | — |
| Injection CI | entrées d'action validées, clés inconnues refusées ; paramètres de stack validés par leur JSON Schema (types, choix, valeurs verrouillées, inconnus refusés) avant d'entrer dans le squelette | — |
| Webhooks entrants | GitLab : secret partagé (16 caractères min.) comparé en temps constant ; GitHub : signature HMAC-SHA256 du corps ; routes hors `/api/v1`, sans session ; repos réabonnés quand le secret change | — |
| Configuration | une config invalide est refusée, la précédente reste en place ; la raison du refus n'est montrée qu'aux admins (page Administration), sans valeur de secret | — |
| Traçabilité | `audit_log` des actions mutantes, avec l'utilisateur OIDC et son adresse | — |
| `/metrics` | public | à protéger au niveau réseau |

## Décisions

| Décision | Pourquoi | Alternative écartée |
|---|---|---|
| IDP maison en Go plutôt que Backstage | léger, un binaire, code maîtrisé de bout en bout ; Backstage demande un monorepo TypeScript à recompiler à chaque plugin | Backstage, SaaS (Port, Cycloid) |
| Stacks en dossiers autonomes dans Git (`stack.yaml` + `skeleton/`) | ajouter une stack = pousser un dossier ; même modèle que les templates Backstage | stacks dans la config, figées au démarrage |
| « Stack » plutôt que « golden path » | un seul mot dans l'interface, la doc et le format ; il dit « une appli et son infra » (l'infra arrive en v0.13), comme chez Cycloid. L'ancien format (`template.yaml`, `kind: GoldenPath`, section `templates`) est lu jusqu'à la v1.0, signalé dans Administration | garder « golden path » (le concept du platform engineering, moins parlant pour l'infra) |
| Paramètres en YAML maison, traduits en JSON Schema | un admin DevOps les écrit sans connaître JSON Schema ; le schéma généré valide (un validateur éprouvé, pas du code maison) et sert aux clients de l'API | JSON Schema écrit à la main (illisible), validation maison seule |
| Catalogue officiel embarqué dans le binaire | versionné avec Symphony, aucun accès réseau ni jeton GitHub, marche hors ligne ; l'admin choisit ce qu'il expose | repo public séparé lu par Git (un jeton, une version à épingler, une instance GitLab sans accès à GitHub) |
| Infra par manifests d'opérateurs en GitOps d'abord (CloudNativePG) | testable de bout en bout dans la démo k3s et en CI, sans compte cloud ; aucun identifiant chez Symphony (l'opérateur crée les secrets dans le cluster) ; une infra par environnement, supprimée avec le projet | OpenTofu d'abord (demande un cloud pour être testé ; viendra avec les cibles AWS et Scaleway) |
| Manifests de la stack copiés dans le repo de déploiement | Argo CD ne lit qu'un repo, avec les identifiants qu'il a déjà ; le contrat (valeurs Helm, noms Kustomize) garde la main de Symphony sur l'image, les réplicas et les secrets | Application qui lit le repo du service (un accès Argo CD par repo) |
| Config dans Git, bootstrap local minimal | la plateforme se pilote par commits ; le bootstrap règle le problème de poule et d'œuf | tout en local ; tout dans Git sans bootstrap (impossible) |
| Rechargement de la config par polling (15 s) | simple, sans webhook entrant à exposer pour les repos de la plateforme | webhooks Git |
| Tâches en base, étapes rejouables | une opération longue survit à un redémarrage ; pas de file externe à opérer | Redis, NATS, Temporal (une brique de plus pour un seul binaire) |
| Webhooks de pipeline + sondage de secours | l'avancement arrive en direct, sans dépendre d'un webhook qui peut se perdre ou ne pas exister (GitHub en local) | sondage seul (lent), webhooks seuls (fragile) |
| Server-Sent Events plutôt que WebSocket | flux serveur → navigateur seulement, HTTP simple, cookie de session et reconnexion natifs | WebSocket, sondage par le front |
| `Registry.Replace` plutôt que redémarrer | pas de coupure ; les requêtes en cours finissent avec leurs providers | redémarrage du processus |
| API Docker sur le socket, sans SDK | quelques appels HTTP suffisent, pas de dépendance lourde | SDK Docker |
| Catalogue en mémoire, source dans Git | Git est la source de vérité, la base ne garde que l'état propre à Symphony | catalogue en base |
| Fiche dans le repo du projet | elle évolue avec le code, par l'équipe qui le connaît, avec ses droits sur le repo ; plus de fiche sans repo ni pipeline derrière | dossier `services/` du repo de config (versions ≤ 0.5) |
| Fiche au format Backstage, ajouts en annotations et champs de spec | une entreprise qui a Backstage garde ses fiches et ses outils ; aucun format propre à apprendre | format maison (`symphony/v1`, lu jusqu'à la v1.0) ; un fichier Symphony à côté de celui de Backstage |
| Découverte déclarée dans la configuration, en lecture seule | Git reste la source (pas d'import cliqué à refaire) ; aucun droit d'écriture sur les repos découverts ; un projet orphelin est signalé, pas retiré (une erreur passagère ne fait rien disparaître) | import ponctuel dans l'interface ; webhook posé dans chaque repo scanné |
| Backend-for-Frontend pour l'OIDC | le navigateur ne détient aucun jeton (pas de vol par XSS), le front n'a pas de bibliothèque OIDC ; recommandé par l'IETF pour les applications navigateur | jetons gérés par la SPA (oidc-client) |
| Session dans un cookie signé, rôle recalculé à chaque requête | pas de stockage de sessions ; un changement de groupes → rôles dans la config s'applique sans reconnexion | sessions en base |
| Secrets et identité en providers | Vault, OpenBao, un autre coffre ou un autre IdP s'ajoutent comme les autres outils ; la config désigne des secrets par référence, sans savoir où ils vivent | client Vault et Keycloak codés en dur |
| Références `secret://` résolues dans une copie, tout ou rien | aucune valeur en clair dans la config gardée en mémoire ; un coffre injoignable laisse la config en place au lieu de retirer des providers | résolution provider par provider (perte silencieuse de providers) |
| Bascule Docker sur le `HEALTHCHECK` de l'image | le service est vraiment prêt (pas seulement démarré) avant que l'ancien disparaisse ; la stack décide de la sonde | délai fixe (3 s, insuffisant pour une appli lente, trop long pour une rapide) |
| Découverte OIDC paresseuse et retentée | Symphony démarre même si l'IdP est absent ; `discovery_url` sépare l'URL interne de l'issuer vu par le navigateur | découverte au démarrage, bloquante |
| Kubernetes via Argo CD | Symphony écrit des manifests, Argo CD applique et corrige les dérives : fidèle au principe de délégation, et Symphony n'a aucun droit sur le cluster | appels directs à l'API Kubernetes (client-go) |
| Un repo de déploiement, un dossier par service | un seul endroit à brancher sur Argo CD, un historique de tous les déploiements | un repo par service, ou les manifests dans le repo du code (un commit de déploiement relancerait la CI) |
| Kustomize pour ce qui change | arrêter ou relancer modifie un petit fichier typé, relu puis réécrit ; les autres manifests restent tels qu'écrits | Helm (un chart pour quatre fichiers), réécriture complète à chaque action |
| k3s dans le `docker compose` de la démo | la distribution qu'utilise k3d, sans outil de plus à installer ; un relais (`gitlab-proxy`) partage le réseau de k3s pour que containerd et Argo CD joignent GitLab aux mêmes adresses que le reste de la démo | k3d (un CLI de plus, réseau séparé de GitLab) |

## Écarts avec la proposition KoreIDP

| KoreIDP | Symphony |
|---|---|
| Binaire unique avec la SPA embarquée | **fait** (`go:embed`, image `scratch`) |
| Configuration déclarative dans un repo Git | **fait**, avec rechargement à chaud |
| Drivers Go stricts, pas de plugins | **fait** (6 interfaces, 12 implémentations) |
| L'IDP n'exécute pas de `kubectl apply` | **fait** pour Kubernetes (runtime `argocd`) ; le runtime Docker local déploie directement, pour la démo |
| OIDC exclusif, RBAC par groupes | **fait** (Keycloak en démo, tout fournisseur OIDC ; sans section `auth`, mode développement) |
| Vault pour les secrets | **fait** : provider `vault` (KV, AppRole), références `secret://`, rotation à chaud |
| Tâches asynchrones PENDING → RUNNING → SUCCESS via webhooks CI | **fait** : tâches en base, webhooks GitLab, sondage de secours, SSE |
| `/metrics` Prometheus | **fait** (API, providers, rechargements) |
| Repos d'infra séparés des repos applicatifs | **fait** pour Kubernetes : manifests dans `symphony-deployments` |
| Harbor, Jenkins, Terraform | hors périmètre : registry GitLab, GitLab CI / GitHub Actions |
| Haute disponibilité multi-réplicas | hors périmètre : le catalogue en mémoire et l'exécution des tâches (un seul runner, abonnés SSE locaux) supposent une instance |
