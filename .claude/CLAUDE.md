# Symphony — Contexte projet

## Ce que c'est, en une phrase

Une Internal Developer Platform (IDP) open source, auto-hébergée, en
binaire Go unique : elle donne à un développeur un golden path complet
(repo + CI + registre + namespace + déploiement) en quelques clics, et à
un admin DevOps une plateforme qu'il configure en YAML sans jamais
toucher au code Go ni faire de web.

## Référence produit : Symphony est un Cycloid open source, auto-hébergé

**Cycloid** est la référence produit la plus proche de Symphony : c'est
une IDP SaaS qui combine un Service Catalog (stacks = golden paths),
une gestion d'environnements (recette / préprod / prod), une vue pipeline,
du RBAC multi-tenant, du GitOps, un inventaire des ressources, et de la
gestion de coûts. C'est le produit dont Symphony veut être l'équivalent
open source, auto-hébergé, admin-friendly dès le départ.

### État du MVP de Symphony vs fonctionnalités Cycloid

Cette table sert de roadmap de comparaison. Les agents d'architecture et
d'implémentation doivent s'y référer avant de proposer des features.

| Fonctionnalité Cycloid | Symphony MVP | Statut |
|---|---|---|
| Service Catalog / golden paths | `config/golden-paths/` + `internal/templates/` | ✅ Implémenté |
| Stack templates déclaratifs (YAML) | `golden-path.yaml` + rechargement à chaud | ✅ Implémenté |
| Création self-service d'un projet | `POST /api/v1/projects` → repo + CI + registre + namespace | ✅ Implémenté |
| Vue pipeline par projet | `GET /api/v1/projects/:name/pipelines` + réconciliation 30s | ✅ Implémenté |
| Déploiement d'environnements (recette) | Docker via golden path CI | ✅ Implémenté (Docker) |
| GitOps — sync config infra dans repo | `internal/gitops/sync.go` | ✅ Implémenté |
| Auth fédérée (OIDC) | `internal/auth/` + Dex/Keycloak/Azure AD | ✅ Implémenté |
| Audit trail (qui fait quoi) | `db.Log()` — user hardcodé "system" pour l'instant | ⚠️ Partiel |
| Vue état déploiements par env | `GET /api/v1/projects/:name/deployments` | ⚠️ Partiel |
| RBAC (rôles par groupe/projet) | Non implémenté — groupes OIDC présents, mapping à faire | ❌ Non implémenté |
| Inventaire des ressources actives | Non implémenté | ❌ Non implémenté |
| Multi-cloud / Kubernetes | Docker seulement en MVP, interface prête pour K8s | ❌ Post-MVP |
| Gestion des coûts par env/équipe | Non implémenté | ❌ Post-MVP |
| Quotas et politiques de ressources | Non implémenté | ❌ Post-MVP |
| Gestion multi-organisation / tenants | Non implémenté — Symphony est single-tenant pour le MVP | ❌ Post-MVP |
| Wizard d'initialisation admin | `internal/api/setup.go` + `Setup.vue` | ✅ Implémenté |
| Lien externe vers monitoring (Prometheus) | `monitoring_url` dans `golden-path.yaml`, lien dans UI | ✅ Implémenté |

**Principe de priorisation** : avant d'implémenter une feature non cochée,
vérifier qu'elle sert directement l'un des deux personae (dev / admin DevOps
solo) dans le contexte d'une entreprise de taille moyenne (10–200 devs),
pas une fonctionnalité enterprise de Cycloid pensée pour 1000+ devs avec
une plateforme team dédiée.

## Le problème que Symphony résout (et celui qu'il évite de recréer)

Les devs jonglent avec trop d'outils (forge Git, CI, registre,
Kubernetes, monitoring) pour des tâches répétitives. Les alternatives
existantes ratent l'une des deux cibles :
- **Backstage** : trop complexe à opérer, pas pensé pour un admin DevOps
  solo — c'est un produit pensé pour une plateforme team dédiée.
- **Cycloid / Port (SaaS)** : payant, lock-in, problème de souveraineté
  des données pour une entreprise qui veut s'auto-héberger.

Symphony vise les deux cibles à la fois : **user-friendly pour le dev**,
**admin-friendly pour le DevOps solo qui ne veut pas faire du web**.

## Les deux personae à satisfaire — tout arbitrage technique s'évalue contre eux

### Le développeur
Veut : créer un projet en quelques clics avec une base solide (CI/CD
préconfiguré, bonnes pratiques par défaut), voir l'état de ses pipelines
et déploiements sans changer d'onglet, déployer une recette à 2 instances
pour tester sa feature sans connaître Kubernetes en détail, accéder en un
clic aux outils plus poussés (Prometheus, logs) seulement s'il en a
besoin.
Ne veut pas : apprendre Harbor, apprendre Terraform, gérer lui-même des
manifests Kubernetes, naviguer entre 5 outils pour comprendre l'état de
son service.

### L'admin DevOps
Veut : une plateforme qu'il configure en YAML, des templates de golden
path qu'il peut adapter au contexte de son entreprise, un seul binaire à
opérer (pas un mesh de microservices ni un système de plugins versionnés
à la Jenkins).
Ne veut pas : écrire du code Go pour ajouter un golden path, gérer une
UI web pour la config, qu'une montée de version d'un outil tiers casse
Symphony entier, être lui-même un "admin API" qui exécute des actions
dangereuses à la main sur l'infra de prod.

**Toute proposition d'architecture ou de feature doit être confrontée à
ces deux profils. Si une idée sert l'un au détriment de l'autre sans
raison explicite, c'est un signal d'alerte à soulever, pas à résoudre
silencieusement.**

## Décisions d'architecture déjà tranchées (CTO call — à ne pas rouvrir sans raison sérieuse)

### 1. Monolithe modulaire, pas microservices

Un seul binaire Go, frontend Vue compilé et embarqué. Raisons :
- Toute la complexité du produit est dans les intégrations externes (6+
  outils tiers), pas dans des domaines métier internes qui justifieraient
  un découpage en services scalables indépendamment.
- La cible admin (DevOps solo) a besoin d'un seul artefact à déployer —
  lui imposer un mesh de services contredirait directement la promesse
  "admin friendly".
- Le problème que pose Backstage n'est pas qu'il est monolithique, c'est
  que son système de plugins runtime est mal maîtrisé. Symphony évite ce
  problème par construction (voir point 2), pas en éclatant en
  microservices.

### 2. Drivers compilés via interfaces Go strictes — jamais de plugins dynamiques runtime

Chaque intégration tierce (GitLab, Harbor, Kubernetes...) est une
implémentation d'une interface définie dans
`internal/providers/interfaces.go` (`SCMProvider`, `CIProvider`,
`RegistryProvider`, `DeployProvider`), compilée dans le binaire. Pas de
`.so`, pas de chargement dynamique, pas de marketplace de plugins tiers.

C'est ce qui permet à la fois : un seul bloc à déployer (réponse à la
peur "Jenkins invivable"), et une vraie abstraction extensible sans
réécrire le core à chaque nouvel outil. Voir le skill `driver-pattern`
pour l'implémentation concrète.

### 3. Provisioning direct, exécution toujours déléguée — la règle centrale du produit

Deux catégories d'actions, traitées différemment :

**Provisioning (appel API direct, synchrone, assumé par Symphony) :**
créer un repo, créer un namespace Kubernetes, créer une entrée Harbor,
créer un projet CI. Ce sont des opérations de *registration* :
idempotentes, réversibles, à blast radius faible. Symphony les exécute
lui-même via ses drivers, immédiatement, parce que la rapidité fait
partie de l'expérience golden path promise au dev.

**Exécution applicative (toujours déléguée à un pipeline externe,
asynchrone) :** build d'image, déploiement, `terraform apply`,
modification de ressources cloud vivantes. Symphony ne le fait JAMAIS
lui-même. Il pousse la configuration nécessaire (manifeste, pipeline
config) dans le repo concerné et déclenche le pipeline existant
(GitLab CI, Jenkins...), qui exécute. Symphony observe ensuite le
résultat (webhook ou polling) et le reflète dans son state tracking
(PENDING → RUNNING → SUCCESS/FAILED).

**Pourquoi cette ligne précise et pas "tout en direct" ou "tout
délégué"** : "tout en direct" recrée le risque qu'une montée de version
Terraform/Kubernetes casse Symphony lui-même au runtime — exactement le
risque nommé dans la vision produit. "Tout délégué" (même la création
d'un repo vide) ajoute de la latence et de la fragilité à une opération
qui devrait être quasi instantanée, ce qui contredit la promesse
"user-friendly" pour le dev. La ligne de partage suit la nature du
risque : provisioning = risque faible et réversible → direct ; exécution
applicative = risque réel sur un système vivant → toujours délégué.

Si une feature semble nécessiter d'exécuter directement quelque chose qui
ressemble à de l'exécution applicative (ex: Symphony qui ferait lui-même
un `kubectl apply` pour déployer), c'est un signal d'architecture à
remonter explicitement, jamais à implémenter en silence "pour aller plus
vite".

### 4. Stateless — toute la mémoire vit en PostgreSQL ou en YAML

Le process Symphony ne garde aucun état métier en mémoire entre deux
requêtes. Tâches asynchrones, statuts, audit trail → PostgreSQL. Config
des intégrations et des golden paths → fichiers déclaratifs. Le process
doit pouvoir redémarrer ou scaler horizontalement sans rien perdre. Voir
le skill `state-machine-conventions`.

### 5. Templates de golden path : déclaratifs, rechargeables sans recompiler

C'est l'engagement le plus structurant pour la promesse "admin
friendly" : un admin ajoute ou modifie un golden path en éditant des
fichiers (templates + config), pas en écrivant du Go. Une route de
rechargement à chaud doit exister.

**Ce point est implémenté** : `internal/templates/loader.go` charge les
golden paths depuis `config/golden-paths/` via `text/template`, expose
`RenderFiles()` / `RenderCI()`, et `POST /api/v1/templates/reload`
recharge à chaud sans redémarrage. Les 4 golden paths (go / python /
node / java rest-api) sont déclaratifs dans `config/golden-paths/`. Un
admin ajoute un nouveau golden path en créant un dossier + `golden-path.yaml`
+ fichiers templates — sans toucher au Go.

La question ouverte #3 ci-dessous est donc résolue pour le design de
base. Ce qui reste à clarifier : la gestion des erreurs de template
(fichier manquant, variable inconnue), et la validation du schéma
`golden-path.yaml` à l'entrée du loader.

## Périmètre du MVP (resserré volontairement)

Un seul outil par catégorie pour le MVP, mais toujours derrière
l'interface générique — ajouter un outil plus tard ne doit jamais
demander de toucher au core, seulement d'ajouter un nouveau package
driver.

| Catégorie | MVP | Prévu plus tard (via la même interface) |
|---|---|---|
| Forge de code | GitLab | GitHub |
| CI/CD | GitLab CI | Jenkins, GitHub Actions |
| Registre d'artefacts | GitLab Registry | Harbor, Nexus |
| Déploiement | Docker (local/dev) | Kubernetes, Terraform (AWS/Azure/GCP) |
| Identité | OIDC (Keycloak/Azure AD/Okta), fédérant l'AD/LDAP existant | — (déjà la cible finale, pas d'étape intermédiaire prévue) |
| Monitoring | Lien externe vers Prometheus existant | Intégration plus poussée si besoin réel |

Ne pas ajouter un deuxième provider par catégorie avant que le premier
soit solide et utilisé réellement — l'abstraction est prête, mais la
multiplier prématurément dilue l'effort sans valeur immédiate.

## Le golden path, déroulé concret (la fonctionnalité centrale du produit)

1. Le dev choisit un type de projet (ex: "REST API en Go") dans l'UI.
2. Symphony **provisionne directement** (voir règle ci-dessus) : un repo
   GitLab, un projet CI, une entrée registre, un namespace de
   déploiement.
3. Symphony pousse dans le repo le contenu du golden path : code de
   base, pipeline CI préconfiguré (tests, build, push image), templates
   de déploiement — le tout avec les bonnes pratiques de l'entreprise
   déjà en place, pas à reconstruire par le dev.
4. Le dev choisit un groupe existant ou en crée un (si droits suffisants)
   pour que son équipe ait une vue partagée sur le projet.
5. Depuis la page du projet, le dev voit : lien repo, derniers pipelines
   (test/build/déploiement), état des déploiements par environnement
   (recette/préprod/prod), métriques de base choisies par l'équipe, avec
   lien direct vers Prometheus pour aller plus loin.
6. Le dev peut, sans quitter la page : déclencher un pipeline sur sa
   branche, déployer une recette à N instances pour tester sa feature
   (action simple, pas besoin de connaître Kubernetes), détruire cette
   recette une fois fini (ou la laisser, selon la politique de
   l'équipe — voir "Questions ouvertes" sur qui décide).
7. Plus tard, le dev ou le chef de projet peut étendre le projet (ex:
   ajouter un bucket S3 + un environnement de recette EC2) via une
   action déclarative, sans réécrire de Terraform à la main — le golden
   path encode déjà la bonne pratique pour ce type d'ajout.

Ce flux est la référence : toute feature proposée doit se situer
clairement dans une de ces étapes, ou justifier pourquoi elle en ajoute
une nouvelle.

## Architecture du code

- `cmd/symphony/` : entrypoint uniquement, pas de logique métier.
- `internal/api/` : handlers REST, minces — délèguent à `database` et
  `providers`. Voir skill `api-contract-conventions`.
- `internal/providers/interfaces.go` : le contrat des drivers. Voir
  skill `driver-pattern` avant toute modification.
- `internal/database/` : state machine des tâches asynchrones
  (PENDING → RUNNING → SUCCESS/FAILED), audit trail. Voir skill
  `state-machine-conventions`.
- `internal/gitops/` : sync vers le repo d'infra généré, jamais vers le
  repo applicatif du dev.
- `internal/catalog/` + `internal/templates/` : golden paths et
  catalogue de services. Zone la moins mature actuellement — voir skill
  `golden-path-templating`.

## Sécurité — non négociable dès le MVP

- Authentification via **OIDC exclusivement** (Keycloak, Azure AD, Okta
  ou équivalent). Symphony ne parle jamais LDAP/AD directement — c'est
  le fournisseur OIDC qui fédère l'annuaire d'entreprise existant en
  amont. Jamais de mot de passe utilisateur stocké par Symphony. Les
  permissions dans l'UI sont calquées sur les groupes/claims du token
  OIDC, pas sur une table de permissions maintenue en parallèle.
- Aucun secret en dur dans `config/integrations.yaml` — toujours une
  variable d'environnement ou un pointeur vers un coffre-fort
  (Vault/Secrets Manager).
- Tokens de service des drivers à scope minimal — un token GitLab de
  Symphony n'a accès qu'au groupe qu'il gère, jamais à l'instance
  entière, même pour les opérations de provisioning direct.
- Un flag de confort dev (ex: `SYMPHONY_DEV_MODE`) ne relâche jamais
  qu'une exigence d'**authentification** (qui tu es) — jamais une
  exigence de **configuration** du produit (ce que Symphony peut faire).
  `SYMPHONY_DEV_MODE=1` injecte un faux utilisateur admin, mais ne doit
  jamais dispenser de configurer les providers via le wizard : sinon le
  wizard n'est jamais réellement exercé en dev/démo, et les bugs qu'il
  aurait attrapés (scopes de token incorrects, dispatch par type cassé)
  ne sont découverts qu'en environnement réel. Repéré via un vrai bug :
  `setupStatus` traitait `devMode` comme équivalent à "providers
  configurés", ce qui cachait le wizard pendant toute une session de
  démo (voir historique git `internal/api/setup.go`).

## Stack technique

Go 1.25, PostgreSQL (driver cible : `pgx`, pas `lib/pq` qui n'est plus
maintenu), chi router, Vue 3 + Vite pour le frontend, OpenTelemetry /
Prometheus pour l'observabilité de Symphony lui-même.

## Comment travailler sur ce projet avec Claude Code

- Avant toute décision structurante (nouvelle dépendance, nouveau
  pattern, nouveau type de provider), consulter l'agent
  `symphony-architect` plutôt que d'avancer sur une supposition.
- Le projet est encore au stade de fondations — il est attendu et
  souhaité que des choix actuels soient challengés et refactorés s'ils
  ne tiennent pas la route face aux deux personae (dev / admin) ou aux 5
  décisions d'architecture ci-dessus. Ne pas chercher à préserver du
  code existant par défaut si l'architecture l'exige.
- Toute zone marquée "Questions ouvertes" ci-dessous doit être clarifiée
  explicitement avec l'utilisateur avant d'être implémentée — ne pas
  deviner silencieusement.

## Questions ouvertes (intentionnellement non tranchées — à clarifier avant d'implémenter ces zones)

1. **Qui décide de la politique de rétention des environnements de
   recette** (auto-destroy après N heures vs laissé à la main du dev/de
   l'équipe) ? La vision le laisse à "la politique de l'entreprise" —
   ça doit devenir un paramètre de config golden path, pas un
   comportement codé en dur.
2. **Modèle de permissions par groupe** : la vision mentionne "groupe"
   comme unité de visibilité sur un projet, mais ne précise pas la
   granularité des droits (qui peut déployer en prod vs juste voir les
   métriques). À clarifier avant de concevoir le RBAC — sera porté par
   les groupes/claims OIDC (voir section Sécurité), mais le mapping
   exact groupe → permission reste à définir.
3. ~~**Design concret du templating de golden path**~~ — **Résolu** :
   `internal/templates/loader.go` + `config/golden-paths/` + rechargement
   à chaud `POST /api/v1/templates/reload`. Ce qui reste à préciser :
   validation du schéma `golden-path.yaml` à l'entrée du loader, et
   comportement en cas de template invalide (rejet ou log + skip).
