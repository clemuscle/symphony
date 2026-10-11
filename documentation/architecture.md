# Architecture de Symphony

Symphony est une **Internal Developer Platform** (IDP) : un backend Go et une SPA Vue 3 qui
s'appuient sur les outils existants (GitLab ou GitHub, leur CI, un registry d'images, un
runtime de conteneurs) pour qu'un développeur crée un service prêt à déployer en un clic,
depuis un *golden path* validé par l'équipe plateforme.

Ce document décrit l'architecture actuelle et les décisions qui la fondent. Ce qui reste à
faire est dans [ROADMAP.md](../ROADMAP.md). Symphony est né d'une proposition d'architecture
appelée « KoreIDP » ; la section [Écarts avec KoreIDP](#écarts-avec-la-proposition-koreidp)
dit ce qui en a été repris, adapté ou écarté.

## Principes

1. **Tout se pilote par Git.** La configuration de Symphony, les golden paths et le
   catalogue de services vivent dans des repos Git, relus toutes les 15 s. Le binaire est
   immuable ; changer la plateforme, c'est pousser un commit.
2. **Des drivers, pas des plugins.** Chaque outil externe est derrière une interface Go
   stricte (`SCMProvider`, `CIProvider`, `RegistryProvider`, `RuntimeProvider`). Pas de
   chargement dynamique : ajouter un outil, c'est implémenter une interface.
3. **Déléguer l'exécution.** Symphony n'exécute pas les builds : il pousse un fichier CI et
   laisse la CI de la plateforme travailler. Le déploiement Docker local est l'exception
   assumée (voir les écarts) ; le déploiement Kubernetes passera par Argo CD.
4. **Aucun secret dans la configuration.** Les fichiers ne contiennent que des noms de
   variables d'environnement (`token_env`).
5. **Rester léger.** Pas de SDK lourd (l'API Docker est appelée directement sur le socket),
   pas d'ORM, un seul binaire (front embarqué, image d'environ 10 Mo) et PostgreSQL.

## Vue d'ensemble

```
                    ┌──────────────────────────────┐
                    │  SPA Vue 3 (catalogue,       │
                    │  projets, déploiements)      │
                    └──────────────┬───────────────┘
                                   │ REST /api/v1
┌──────────────────────────────────▼───────────────────────────────────┐
│ Symphony (Go)                                                        │
│                                                                      │
│  api/ (chi, handlers)   gitops/Syncer      gitops/TemplateLoader     │
│                         catalogue (15 s)   golden paths (15 s)       │
│  configWatcher (15 s)   catalogue.Store    database (PostgreSQL)     │
│                                                                      │
│  provider.Registry : SCM │ CI │ Registry │ Runtime                   │
└──────┬──────────────┬───────────┬──────────────┬─────────────────────┘
       ▼              ▼           ▼              ▼
  GitLab / GitHub  GitLab CI /  Registry      Docker Engine
  (repos)          Actions      GitLab        (socket unix)
```

### Les trois repos Git

| Repo | Contenu | Relu |
|---|---|---|
| `root/symphony-config` | `symphony.yaml` (providers, catalogue, templates) et `services/` (catalogue) | toutes les 15 s, appliqué à chaud |
| `root/symphony-templates` | `templates/<nom>/` : `template.yaml`, template CI, `skeleton/` | toutes les 15 s |
| un repo par service créé | le code du service, généré depuis le golden path | — |

Le `symphony.yaml` local n'est qu'un **bootstrap** (section `config_source`) : comment
joindre GitLab et où lire la configuration. C'est le seul fichier qui demande un
redémarrage, comme l'`app-config` de Backstage.

## Composants

| Paquet | Rôle |
|---|---|
| `cmd/symphony` | Composition : bootstrap et source de config (`config.go`), construction des providers (`providers.go`), assemblage (`main.go`) |
| `internal/config` | Types de `symphony.yaml` et décodage (`${VAR}` remplacées par l'environnement) |
| `internal/provider` | Interfaces, `Registry` (providers par id, remplaçable d'un bloc) et implémentations : `scm/gitlab`, `scm/github`, `ci/gitlabci`, `ci/githubactions`, `registry/gitlabregistry`, `runtime/docker`, `runtime/kubernetes` (stub) |
| `internal/gitops` | `Syncer` (catalogue) et `TemplateLoader` (golden paths) |
| `internal/catalogue` | Store en mémoire des services (`kind: Service`) |
| `internal/database` | PostgreSQL via `database/sql` : `projects`, `pipelines`, `deployments`, `audit_log` ; migrations embarquées |
| `internal/api` | Routeur chi, handlers par domaine, `/healthz` |
| `internal/metrics` | Métriques Prometheus sur `/metrics` ; `internal/provider/instrumented` enveloppe chaque provider pour mesurer ses appels |
| `internal/web` | Front compilé embarqué (`go:embed`), avec repli sur `index.html` pour les routes Vue |
| `frontend/` | Vue 3 + Vite : pages Catalogue, Projets, Déploiements |

## Flux principaux

### Créer un projet depuis un golden path

1. Résoudre le golden path, son SCM et son CI ; refuser un CI qui ne tourne pas sur ce SCM
   (`CheckCompatible` : pas de GitLab CI sur un repo GitHub).
2. Lire le skeleton et le template CI **avant** de créer le repo, et remplacer les
   placeholders (`{{SERVICE_NAME}}`, `{{PORT}}`…).
3. Créer le repo, puis pousser skeleton et CI en **un seul commit** : ce push lance la
   première pipeline (build, tests, image dans le registry).
4. Enregistrer le projet en base, écrire sa fiche dans `symphony-config/services/` (le
   catalogue le voit aussitôt), journaliser dans l'audit.

### Recharger la configuration à chaud

Le `configWatcher` relit `symphony.yaml` dans Git. Si le contenu a changé et qu'il est
valide, un nouveau `Registry` est construit puis substitué d'un bloc
(`Registry.Replace`) : les handlers gardent le même pointeur et voient soit l'ancien jeu de
providers, soit le nouveau. Le syncer et le template loader sont repointés
(`SetSource`) et rechargés. Une configuration illisible ou invalide est refusée et la
précédente reste en place.

### Déployer

1. L'UI préremplit l'image et les tags depuis le registry du projet.
2. Le runtime Docker **tire l'image d'abord** (avec l'auth du registry, envoyée seulement
   si l'image y est hébergée) : si le pull échoue, l'ancien conteneur continue de tourner.
3. Bascule avec rollback : le nouveau conteneur démarre à côté (`<nom>-next`), l'ancien est
   arrêté ; si le nouveau tourne encore après 3 s il remplace l'ancien, sinon l'ancien
   redémarre et l'erreur contient les derniers logs du nouveau.

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

Plus les métriques du runtime Go et du processus. Les routes sont étiquetées par leur motif
(`/api/v1/projects/{name}`), jamais par le chemin, pour garder un nombre de séries borné.
Les appels aux providers sont mesurés par des décorateurs posés dans `buildProviders` : les
implémentations n'en savent rien.

## Sécurité

| Sujet | Aujourd'hui | Prévu |
|---|---|---|
| Authentification | aucune ; CORS `*` | OIDC (Keycloak), CORS restreint — v0.3 |
| Autorisations | aucune | rôles déduits des groupes OIDC — v0.3 |
| Secrets | variables d'environnement, jamais dans les fichiers | Vault — v0.6 |
| Fuite de token | l'auth du registry n'est envoyée qu'au registry qui héberge l'image | — |
| Injection CI | entrées d'action validées, clés inconnues refusées | — |
| Configuration | une config invalide est refusée, la précédente reste en place | — |
| Traçabilité | `audit_log` des actions mutantes (utilisateur `system`) | utilisateur OIDC réel — v0.3 |

## Décisions

| Décision | Pourquoi | Alternative écartée |
|---|---|---|
| IDP maison en Go plutôt que Backstage | léger, un binaire, code maîtrisé de bout en bout ; Backstage demande un monorepo TypeScript à recompiler à chaque plugin | Backstage, SaaS (Port, Cycloid) |
| Golden paths en dossiers autonomes dans Git (`template.yaml` + `skeleton/`) | ajouter un template = pousser un dossier ; même modèle que les templates Backstage | golden paths dans la config, figés au démarrage |
| Config dans Git, bootstrap local minimal | la plateforme se pilote par commits ; le bootstrap règle le problème de poule et d'œuf | tout en local ; tout dans Git sans bootstrap (impossible) |
| Rechargement par polling (15 s) | simple, sans webhook entrant à exposer | webhooks Git (prévus pour la CI en v0.5) |
| `Registry.Replace` plutôt que redémarrer | pas de coupure ; les requêtes en cours finissent avec leurs providers | redémarrage du processus |
| API Docker sur le socket, sans SDK | quelques appels HTTP suffisent, pas de dépendance lourde | SDK Docker |
| Catalogue en mémoire, source dans Git | Git est la source de vérité, la base ne garde que l'état propre à Symphony | catalogue en base |
| Kubernetes via Argo CD (prévu) | Symphony écrit des manifests, Argo CD applique : fidèle au principe de délégation | appels directs à l'API Kubernetes |

## Écarts avec la proposition KoreIDP

| KoreIDP | Symphony |
|---|---|
| Binaire unique avec la SPA embarquée | **fait** (`go:embed`, image `scratch`) |
| Configuration déclarative dans un repo Git | **fait**, avec rechargement à chaud |
| Drivers Go stricts, pas de plugins | **fait** (4 interfaces, 7 implémentations) |
| L'IDP n'exécute pas de `kubectl apply` | respecté pour Kubernetes (Argo CD, v0.4) ; le runtime Docker local déploie directement, pour la démo |
| OIDC exclusif, RBAC par groupes | prévu en v0.3 (Keycloak) |
| Vault pour les secrets | prévu en v0.6 |
| Tâches asynchrones PENDING → RUNNING → SUCCESS via webhooks CI | prévu en v0.5 |
| `/metrics` Prometheus | **fait** (API, providers, rechargements) |
| Repos d'infra séparés des repos applicatifs | repris pour Kubernetes : manifests dans un repo de déploiement (v0.4) |
| Harbor, Jenkins, Terraform | hors périmètre : registry GitLab, GitLab CI / GitHub Actions |
| Haute disponibilité multi-réplicas | hors périmètre : l'état en mémoire (catalogue, suppressions en cours) suppose une instance |
