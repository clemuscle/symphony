# Architecture de Symphony

Symphony est une **Internal Developer Platform** (IDP) : un backend Go et une SPA Vue 3 qui
s'appuient sur les outils existants (GitLab ou GitHub, leur CI, un registry d'images, un
runtime : Docker ou Kubernetes via Argo CD) pour qu'un développeur crée un service prêt à déployer en un clic,
depuis un *golden path* validé par l'équipe plateforme.

Ce document décrit l'architecture actuelle et les décisions qui la fondent. Ce qui reste à
faire est dans [ROADMAP.md](../ROADMAP.md). Symphony est né d'une proposition d'architecture
appelée « KoreIDP » ; la section [Écarts avec KoreIDP](#écarts-avec-la-proposition-koreidp)
dit ce qui en a été repris, adapté ou écarté.

## Principes

1. **Tout se pilote par Git.** La configuration de Symphony, les golden paths et la fiche
   catalogue de chaque projet vivent dans des repos Git, relus toutes les 15 s. Le binaire est
   immuable ; changer la plateforme, c'est pousser un commit.
2. **Des drivers, pas des plugins.** Chaque outil externe est derrière une interface Go
   stricte (`SCMProvider`, `CIProvider`, `RegistryProvider`, `RuntimeProvider`). Pas de
   chargement dynamique : ajouter un outil, c'est implémenter une interface.
3. **Déléguer l'exécution.** Symphony n'exécute pas les builds : il pousse un fichier CI et
   laisse la CI de la plateforme travailler. Sur Kubernetes, il ne déploie pas non plus : il
   commite des manifests, Argo CD les applique. Le runtime Docker local est l'exception
   assumée (voir les écarts).
4. **Aucun secret dans la configuration.** Les fichiers ne contiennent que des noms de
   variables d'environnement (`token_env`).
5. **Rester léger.** Pas de SDK lourd (l'API Docker est appelée directement sur le socket),
   pas d'ORM, un seul binaire (front embarqué, image d'environ 10 Mo) et PostgreSQL.

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
│  api/ (chi, handlers)   gitops/Syncer      gitops/TemplateLoader     │
│  tasks/Runner ◄── webhooks GitLab          golden paths (15 s)       │
│  (tâches, SSE)          fiches (15 s)                                │
│  configWatcher (15 s)   catalogue.Store    database (PostgreSQL)     │
│                                                                      │
│  provider.Registry : SCM │ CI │ Registry │ Runtime                   │
└──────┬──────────────┬───────────┬──────────────┬──────────┬──────────┘
       ▼              ▼           ▼              ▼          ▼
  GitLab / GitHub  GitLab CI /  Registry      Docker     Argo CD ──► Kubernetes
  (repos)          Actions      GitLab        (socket)   (API)       (k3s en démo)
```

### Les repos Git

| Repo | Contenu | Relu |
|---|---|---|
| `platform/symphony-config` | `symphony.yaml` (providers, templates, authentification…) | toutes les 15 s, appliqué à chaud |
| `platform/symphony-templates` | `templates/<nom>/` : `template.yaml`, template CI, `skeleton/` | toutes les 15 s |
| `apps/<service>` : un repo par projet | le code du service, généré depuis le golden path, et sa fiche `catalog-info.yaml` | la fiche, toutes les 15 s |
| `platform/symphony-deployments` | `apps/<service>/` : manifests Kubernetes écrits par Symphony (runtime `argocd`) | par Argo CD |

Deux groupes en démo : **platform**, tenu par l'équipe plateforme, et **apps**, ouvert aux
développeurs (`providers.scm[].config.namespace` : groupe des repos créés). La fiche d'un
projet est dans **son** repo, pas dans la configuration de la plateforme : son équipe la
modifie par un commit, à côté du code qu'elle décrit (le modèle de `catalog-info.yaml`
chez Backstage). Le catalogue de Symphony, ce sont les golden paths ; ses projets sont ce
qui en est sorti.

Le `symphony.yaml` local n'est qu'un **bootstrap** (section `config_source`) : comment
joindre GitLab et où lire la configuration. C'est le seul fichier qui demande un
redémarrage, comme l'`app-config` de Backstage.

## Composants

| Paquet | Rôle |
|---|---|
| `cmd/symphony` | Composition : bootstrap et source de config (`config.go`), construction des providers (`providers.go`), assemblage (`main.go`) |
| `internal/config` | Types de `symphony.yaml` et décodage (`${VAR}` remplacées par l'environnement) |
| `internal/provider` | Interfaces, `Registry` (providers par id, remplaçable d'un bloc) et implémentations : `scm/gitlab`, `scm/github`, `ci/gitlabci`, `ci/githubactions`, `registry/gitlabregistry`, `runtime/docker`, `runtime/argocd` |
| `internal/gitops` | `Syncer` (fiche `catalog-info.yaml` de chaque projet) et `TemplateLoader` (golden paths) |
| `internal/catalogue` | Store en mémoire des fiches (`kind: Service`) |
| `internal/tasks` | Moteur de tâches : étapes enregistrées en base, attente de pipelines (webhook ou sondage), reprise au démarrage, diffusion des changements (`Hub`, pour le SSE) |
| `internal/webhook` | Réception des événements de pipeline GitLab (`/webhooks/gitlab`, secret partagé) |
| `internal/database` | PostgreSQL via `database/sql` : `projects`, `pipelines`, `deployments`, `tasks`, `audit_log` ; migrations embarquées |
| `internal/api` | Routeur chi, handlers par domaine, `/healthz` |
| `internal/metrics` | Métriques Prometheus sur `/metrics` ; `internal/provider/instrumented` enveloppe chaque provider pour mesurer ses appels |
| `internal/auth` | OIDC (connexion, retour, déconnexion), cookie de session signé, rôles, CSRF, CORS ; `Holder` remplacé d'un bloc au rechargement de la config |
| `internal/web` | Front compilé embarqué (`go:embed`), avec repli sur `index.html` pour les routes Vue |
| `frontend/` | Vue 3 + Vite : pages Projets (avec leur fiche), Catalogue (golden paths), Déploiements, Activité |

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
  sans webhook (GitHub Actions). Au-delà d'une heure, la tâche échoue.
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

### Créer un projet depuis un golden path

Tâche `create_project`, après validation (nom DNS, golden path, compatibilité SCM/CI,
projet inexistant) :

1. **Repo et squelette** : lire le skeleton et le template CI **avant** de créer le repo
   (un template cassé ne laisse pas de repo vide), remplacer les placeholders, créer le
   repo, puis pousser skeleton, CI et fiche `catalog-info.yaml` en **un seul commit**, qui
   lance la première pipeline. La fiche part de celle du skeleton s'il en a une (liens,
   SLO, actions du golden path), et reçoit l'équipe, le tier et le cycle de vie saisis.
2. **Projet et webhook** : enregistrer le projet (le `Syncer` lit alors sa fiche), abonner
   Symphony aux événements de pipeline du repo.
3. **Première pipeline** : attendre sa fin. Quand la tâche réussit, l'image du service est
   dans le registry : le projet est prêt à déployer.

### Recharger la configuration à chaud

Le `configWatcher` relit `symphony.yaml` dans Git. Si le contenu a changé et qu'il est
valide, un nouveau `Registry` est construit puis substitué d'un bloc
(`Registry.Replace`) : les handlers gardent le même pointeur et voient soit l'ancien jeu de
providers, soit le nouveau. Le syncer et le template loader sont repointés
(`SetSource`) et rechargés. Une configuration illisible ou invalide est refusée et la
précédente reste en place.

### Déployer

Tâche `deploy` : le runtime déploie, puis la tâche attend que le service tourne (Argo CD
synchronisé et pods prêts, ou conteneur démarré), 5 min au plus.

1. L'UI préremplit l'image et les tags depuis le registry du projet.
2. Le runtime Docker **tire l'image d'abord** (avec l'auth du registry, envoyée seulement
   si l'image y est hébergée) : si le pull échoue, l'ancien conteneur continue de tourner.
3. Bascule avec rollback : le nouveau conteneur démarre à côté (`<nom>-next`), l'ancien est
   arrêté ; si le nouveau tourne encore après 3 s il remplace l'ancien, sinon l'ancien
   redémarre et l'erreur contient les derniers logs du nouveau.

### Déployer sur Kubernetes (runtime `argocd`, GitOps)

Le runtime d'un projet vient de son golden path (`spec.providers.runtime`) ; le formulaire
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

Les identifiants du registry ne sont jamais écrits dans Git : le cluster tire les images
avec les siens (en démo, `registries.yaml` de k3s avec un jeton `read_registry`). Le
compte Argo CD de Symphony n'a droit qu'aux Applications du projet `default` et à leurs
journaux.

### Se connecter (OIDC, Backend-for-Frontend)

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
alice Owner de `platform` et `apps`, bob Developer et carol Reporter de `apps`. GitLab CE
ne synchronise pas les groupes de l'annuaire (fonction payante) : ces droits sont posés
par le script, pas déduits des groupes Keycloak.

### Actions Day-2

Une fiche de catalogue peut déclarer des actions (« Relancer la pipeline »…) : un appel
webhook, ou une pipeline avec des entrées typées. Les entrées sont validées (bornes, options,
requis) et **les clés non déclarées sont refusées**, pour qu'un appelant ne puisse pas
injecter de variables CI arbitraires.

## Observabilité

`/metrics` (format Prometheus) expose :

| Métrique | Labels | Sert à voir |
|---|---|---|
| `symphony_http_requests_total`, `symphony_http_request_duration_seconds` | `method`, `route` (motif chi), `status` | trafic, latence et erreurs de l'API |
| `symphony_provider_calls_total`, `symphony_provider_call_duration_seconds` | `kind`, `provider`, `method`, `result` | santé et lenteur de GitLab, GitHub, Docker… |
| `symphony_reloads_total`, `symphony_last_successful_reload_timestamp_seconds` | `source` (catalogue, templates, config), `result` (ok, error, refused) | un repo illisible, une config refusée, une synchro arrêtée |
| `symphony_catalogue_services`, `symphony_golden_paths` | — | ce qui est chargé |
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
| Autorisations | rôles `viewer` < `developer` < `admin` déduits des groupes OIDC (configuration), vérifiés par route | rôles par projet |
| CSRF | requête modifiante avec cookie : `Origin` (ou `Sec-Fetch-Site`) doit être Symphony | — |
| CORS | aucune origine tierce par défaut ; liste `cors_origins`, sans cookies | — |
| Secrets | variables d'environnement, jamais dans les fichiers ni dans le repo de déploiement | Vault — v0.6 |
| Argo CD | compte de service à jeton (`apiKey`), limité aux Applications du projet `default` et à leurs journaux | — |
| Fuite de token | l'auth du registry n'est envoyée qu'au registry qui héberge l'image | — |
| Injection CI | entrées d'action validées, clés inconnues refusées | — |
| Webhooks entrants | secret partagé (16 caractères min.), comparé en temps constant ; route hors `/api/v1`, sans session | — |
| Configuration | une config invalide est refusée, la précédente reste en place | — |
| Traçabilité | `audit_log` des actions mutantes, avec l'utilisateur OIDC et son adresse | — |
| `/metrics` | public | à protéger au niveau réseau |

## Décisions

| Décision | Pourquoi | Alternative écartée |
|---|---|---|
| IDP maison en Go plutôt que Backstage | léger, un binaire, code maîtrisé de bout en bout ; Backstage demande un monorepo TypeScript à recompiler à chaque plugin | Backstage, SaaS (Port, Cycloid) |
| Golden paths en dossiers autonomes dans Git (`template.yaml` + `skeleton/`) | ajouter un template = pousser un dossier ; même modèle que les templates Backstage | golden paths dans la config, figés au démarrage |
| Config dans Git, bootstrap local minimal | la plateforme se pilote par commits ; le bootstrap règle le problème de poule et d'œuf | tout en local ; tout dans Git sans bootstrap (impossible) |
| Rechargement de la config par polling (15 s) | simple, sans webhook entrant à exposer pour les repos de la plateforme | webhooks Git |
| Tâches en base, étapes rejouables | une opération longue survit à un redémarrage ; pas de file externe à opérer | Redis, NATS, Temporal (une brique de plus pour un seul binaire) |
| Webhooks de pipeline + sondage de secours | l'avancement arrive en direct, sans dépendre d'un webhook qui peut se perdre ou ne pas exister (GitHub en local) | sondage seul (lent), webhooks seuls (fragile) |
| Server-Sent Events plutôt que WebSocket | flux serveur → navigateur seulement, HTTP simple, cookie de session et reconnexion natifs | WebSocket, sondage par le front |
| `Registry.Replace` plutôt que redémarrer | pas de coupure ; les requêtes en cours finissent avec leurs providers | redémarrage du processus |
| API Docker sur le socket, sans SDK | quelques appels HTTP suffisent, pas de dépendance lourde | SDK Docker |
| Catalogue en mémoire, source dans Git | Git est la source de vérité, la base ne garde que l'état propre à Symphony | catalogue en base |
| Fiche dans le repo du projet | elle évolue avec le code, par l'équipe qui le connaît, avec ses droits sur le repo ; plus de fiche sans repo ni pipeline derrière | dossier `services/` du repo de config (versions ≤ 0.5) |
| Backend-for-Frontend pour l'OIDC | le navigateur ne détient aucun jeton (pas de vol par XSS), le front n'a pas de bibliothèque OIDC ; recommandé par l'IETF pour les applications navigateur | jetons gérés par la SPA (oidc-client) |
| Session dans un cookie signé, rôle recalculé à chaque requête | pas de stockage de sessions ; un changement de groupes → rôles dans la config s'applique sans reconnexion | sessions en base |
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
| Drivers Go stricts, pas de plugins | **fait** (4 interfaces, 7 implémentations) |
| L'IDP n'exécute pas de `kubectl apply` | **fait** pour Kubernetes (runtime `argocd`) ; le runtime Docker local déploie directement, pour la démo |
| OIDC exclusif, RBAC par groupes | **fait** (Keycloak en démo, tout fournisseur OIDC ; sans section `auth`, mode développement) |
| Vault pour les secrets | prévu en v0.6 |
| Tâches asynchrones PENDING → RUNNING → SUCCESS via webhooks CI | **fait** : tâches en base, webhooks GitLab, sondage de secours, SSE |
| `/metrics` Prometheus | **fait** (API, providers, rechargements) |
| Repos d'infra séparés des repos applicatifs | **fait** pour Kubernetes : manifests dans `symphony-deployments` |
| Harbor, Jenkins, Terraform | hors périmètre : registry GitLab, GitLab CI / GitHub Actions |
| Haute disponibilité multi-réplicas | hors périmètre : le catalogue en mémoire et l'exécution des tâches (un seul runner, abonnés SSE locaux) supposent une instance |
