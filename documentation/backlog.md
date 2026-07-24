# Symphony — Backlog (issu du retour du 2026-07-24)

Source : `documentation/retour.txt` (retour brut, 6 points). Analyse confrontée
aux 5 décisions d'architecture, au tableau MVP et aux 3 questions ouvertes de
`.claude/CLAUDE.md`, avec revue de l'agent `architecture-guardian`.

## Méthode de tri

Chaque item est classé :
- 🟢 **Actionnable** — cohérent avec l'architecture actuelle, spec prête ci-dessous.
- 🟡 **Bloqué sur décision** — recoupe une question ouverte ou une décision déjà
  tranchée ; pas de spec tant que ce n'est pas clarifié (règle CLAUDE.md : ne
  pas deviner silencieusement sur les zones marquées "questions ouvertes").
- 🔴 **Différé / hors MVP** — cohérent avec la vision long terme mais dépend
  d'un item 🟡 non résolu, ou sert un cas hors du périmètre 10-200 devs.

---

## EPIC A — Démo réaliste, multi-projets

### A1 🟢 Seed de projets à plusieurs stades d'avancement

**Besoin** : la démo actuelle ne montre qu'un scénario de création à vide.
Le retour demande de pré-charger plusieurs projets à des stades différents
pour que l'utilisateur voie tout de suite la diversité des statuts possibles.

**Spec** :
- Script de seed (`scripts/demo/seed.sh` ou étape ajoutée au script infra
  existant) qui crée N projets golden-path réels via l'API `POST /api/v1/projects`
  (pas d'insertion SQL directe de statuts cosmétiques — les états affichés
  doivent être de vrais états traversés par la state machine
  `PENDING → RUNNING → SUCCESS/FAILED`, cf. `internal/database/deployments.go`
  et `pipelines.go`).
- Viser 3-4 projets couvrant : un projet fraîchement créé (juste provisionné,
  pas encore de pipeline lancé), un projet avec pipeline test+build vert et
  aucune recette déployée, un projet avec une recette déployée et active, un
  projet avec un pipeline en échec (pour montrer l'état FAILED dans l'UI).
- **Garde-fou explicite (sécurité)** : ce seed ne doit dispenser à aucun
  moment le wizard de config providers. `SYMPHONY_DEV_MODE` relâche
  uniquement l'authentification, jamais la config — c'est le bug déjà corrigé
  une fois dans `internal/api/setup.go` (voir historique git). Le seed doit
  s'exécuter *après* que les providers réels (ou de démo) soient configurés,
  jamais en les court-circuitant.

### A2 🟢 Parcours "créer mon propre projet" en démo, avec visibilité complète du pipeline

**Besoin** : au-delà des projets pré-créés, l'utilisateur qui lance la démo
doit pouvoir créer un projet lui-même et suivre tests → build → push
artefact → déploiement.

**Spec** : c'est largement déjà couvert par le golden path + la vue pipeline
existante (`GET /api/v1/projects/:name/pipelines`). Le travail restant est
**documentaire/UX**, pas fonctionnel :
- Vérifier dans `DEMO.md` que le parcours guidé fait explicitement dérouler
  les 4 étapes (test/build/push/déploiement) et pas seulement la création du
  projet.
- Sur la page détail projet (voir B2), s'assurer que chaque étape du pipeline
  a un statut visible distinct (pas juste "running" global) — dépend de ce
  que `CIProvider.GetPipelineStatus` renvoie déjà ; à vérifier avant d'ajouter
  du code.

### A3 🟡 (Futur) L'utilisateur enregistre lui-même un provider dans la démo

**Bloqué sur** : ambiguïté à trancher avec le fondateur. Deux lectures très
différentes :
- Enregistrer une **instance** d'un type de provider déjà compilé dans
  Symphony (ex: une nouvelle URL/token GitLab via le wizard) → cohérent avec
  l'architecture, c'est juste exposer le wizard existant en démo.
- Enregistrer un **type** de provider non compilé (un adaptateur inconnu) →
  contredit frontalement la décision d'architecture n°2 (drivers compilés,
  jamais de plugin dynamique runtime).

**Action requise** : confirmer avec le fondateur laquelle des deux lectures
est visée avant d'écrire une spec. Voir question posée en fin de document.

### A4 🔴 Intégration d'un projet déjà existant (GitHub.com + Docker Hub + AWS)

**Différé**. Suppose (a) plusieurs providers par catégorie (EPIC C, non
tranché) et (b) un mode "adopter un projet existant" qui n'existe pas dans le
produit aujourd'hui — Symphony *provisionne*, il n'*adopte* pas. Cette
feature est cohérente avec la vision long terme mais hors du flux golden path
en 7 étapes documenté dans CLAUDE.md, et sa valeur pour la cible 10-200 devs
(vs. cas "enterprise multi-outils historiques") reste à confirmer. À ne pas
backloguer comme actionnable tant que C2 n'est pas tranché.

---

## EPIC B — Page projets en accueil + page détail par projet

### B1 🟢 La page d'accueil devient la liste des projets

**Constat code** : aujourd'hui `/` pointe vers `Catalogue.vue` (le catalogue
de golden paths), et `Projects.vue` est une route séparée `/projects`
(`frontend/src/router/index.js:19-20`). Le retour demande explicitement que
**la première page soit les projets**.

**Spec** :
- `/` → `Projects.vue` (liste des projets, vue actuelle).
- Le catalogue de golden paths (`Catalogue.vue`) devient accessible depuis un
  point d'entrée clair ("Nouveau projet") plutôt que d'être la page
  d'atterrissage — cohérent avec le fait qu'un dev qui revient sur la
  plateforme veut d'abord voir l'état de ses projets, pas recréer un projet.
- Pas de changement backend.

### B2 🟢 Page détail par projet (route dédiée)

**Constat code** : il n'existe aujourd'hui aucune route `/projects/:name` —
`Projects.vue` (586 lignes) semble tout gérer en une seule vue. Le retour
demande une page à part au clic sur un projet.

**Spec** :
- Nouvelle route `/projects/:name` + composant `ProjectDetail.vue`.
- Réutilise les endpoints déjà existants : pipelines
  (`GET /api/v1/projects/:name/pipelines`), déploiements
  (`GET /api/v1/projects/:name/deployments`).
- `Projects.vue` redevient une liste compacte (nom, statut, langage, lien),
  chaque ligne route vers le détail.

### B3 🟢 Enrichir le détail projet : 5 derniers jobs, images, branches (5 max, défaut en premier)

**Spec** :
- **5 derniers jobs/pipelines** : déjà disponible via l'endpoint pipelines
  existant, limiter/trier côté requête ou côté vue.
- **Images** : lister les images poussées au registre pour ce projet — à
  vérifier si `RegistryProvider` (`internal/providers/interfaces.go`) expose
  déjà un `ListImages` ou équivalent ; sinon c'est une extension d'interface
  à appliquer à l'unique driver actuel (`gitlabregistry`).
- **Branches (5 max, défaut en premier)** : **extension de contrat
  nécessaire**. `SCMProvider` n'expose aujourd'hui aucune méthode de listing
  de branches. Ajouter `ListBranches(project string) ([]Branch, error)` à
  l'interface et l'implémenter dans le driver GitLab (seul driver SCM
  aujourd'hui) — voir skill `adapter-pattern` avant de toucher à l'interface,
  c'est un changement de contrat qui doit rester cohérent si un second driver
  SCM arrive plus tard.
- Tout ceci est de la **lecture seule** via drivers existants → provisioning
  à risque faible, pas d'exécution applicative, rien ne contredit la
  décision n°3.

### B4 🟡 Graphiques / métriques sur la page détail

**Bloqué sur** : le fondateur a lui-même laissé ce point volontairement flou
("à voir si nécessaire, quelles métriques"). CLAUDE.md est explicite : le
monitoring poussé est hors MVP (lien externe Prometheus uniquement). Ne rien
construire tant que la liste de métriques n'est pas choisie — le risque est
de reconstruire un mini-Grafana alors que le produit promet justement de ne
pas le faire.

**Action requise** : si le fondateur veut avancer là-dessus, définir d'abord
la liste fermée de métriques avant toute spec (voir question en fin de
document).

---

## EPIC C — Mode admin + préparation multi-provider

**Décisions du fondateur (2026-07-24)** : bascule admin sur le rôle existant
(pas de RBAC granulaire pour l'instant) ; multi-provider en **design
seulement**, pas d'implémentation dans ce cycle.

### C1 🟢 Mode "super admin" — bascule sur le rôle existant, UI façon GitLab

**Constat code** : une route `/admin` existe déjà, gated par
`state.user?.dev_mode || state.user?.role === 'admin'`
(`frontend/src/router/index.js:38-41`), avec accès à la config providers et
au rechargement de templates (`Admin.vue`). Le besoin exprimé (bouton de
bascule façon GitLab) est donc partiellement déjà couvert par l'existant —
il manque surtout l'affordance visuelle.

**Décision** : on ne construit pas de RBAC par groupe maintenant (question
ouverte n°2 de CLAUDE.md reste ouverte). On améliore uniquement l'UI, en
gardant le gating actuel adossé au claim/role OIDC déjà présent dans le
token — jamais un mécanisme d'élévation de droits côté client.

**Spec** :
- Ajouter un bouton "Mode admin" persistant dans la nav (visible seulement
  si `state.user?.dev_mode || state.user?.role === 'admin'`, même condition
  que le guard de route actuel — pas de nouvelle logique d'autorisation).
- Au clic, bascule vers `/admin` (comportement déjà existant) — mais avec un
  état visuel persistant (bandeau ou indicateur "mode admin actif") tant que
  l'utilisateur navigue dans les routes admin, façon GitLab.
- Aucun changement côté backend, aucun nouveau champ de permission stocké.
- **Ne pas** faire dériver ce bouton vers une logique de permissions
  supplémentaire — s'il faut plus de granularité, ça passe par la question
  ouverte n°2, pas par une extension ad hoc de ce bouton.

### C2 🟢 (Design uniquement) Modèle de config multi-provider par catégorie

**Décision** : ne pas fermer la porte à plusieurs providers par catégorie
(scm: gitlab+github ; déploiement: docker+kube+aws), mais ne rien implémenter
dans ce cycle — MVP reste mono-provider tel que documenté. Ce ticket est un
**livrable de design** (document, pas de code fonctionnel).

**Constat code** : `config/integrations.yaml` porte aujourd'hui un objet
unique par catégorie (`scm:` n'est pas une liste). Le dispatch actuel
(handlers → "le" provider d'une catégorie) suppose l'unicité partout dans
`internal/api/` et `internal/providers/`.

**Spec du livrable** (à produire, pas à coder) :
- Esquisser le schéma cible `integrations.yaml` avec plusieurs entrées par
  catégorie, chacune nommée (ex: `scm: [{name: gitlab-primary, type: gitlab,
  ...}, {name: github-secondary, type: github, ...}]`).
- Définir le mécanisme de dispatch : à quel niveau un projet choisit "son"
  provider parmi ceux disponibles (au moment de la création via le golden
  path ? un défaut par golden path ? un choix explicite dans le wizard de
  création de projet ?). C'est la vraie question de design, pas le schéma de
  config lui-même.
- Lister les points du core qui supposent aujourd'hui l'unicité (grep
  `internal/api/` et `internal/providers/` pour tout endroit qui résout "le"
  SCMProvider/DeployProvider sans paramètre de sélection) pour chiffrer
  l'effort du jour où ce sera implémenté.
- Ce document sert de base pour repasser devant `architecture-guardian` le
  jour où l'implémentation réelle sera décidée — ne pas coder avant ce
  passage.

---

## EPIC D — Environnements structurés + tags

**Décision du fondateur (2026-07-24)** : taxonomie fixe (`recette` /
`preprod` / `prod`) + tags libres en plus, pour affiner (équipe, feature...).

### D1 🟢 `environment` comme champ de première classe + tags libres sur les déploiements

**Constat code** : `deployments` (`internal/database/schema.go:50-61`) n'a
aujourd'hui ni colonne `environment`, ni notion de tag — seul un champ texte
`recette_name` distingue implicitement les recettes.

**Spec** :
- Migration : ajouter `environment VARCHAR(20) NOT NULL DEFAULT 'recette'
  CHECK (environment IN ('recette','preprod','prod'))` à la table
  `deployments` (suivre le style additif déjà en place dans `schema.go`,
  cf. `ALTER TABLE ... ADD COLUMN IF NOT EXISTS`).
- Ajouter `tags TEXT[]` (Postgres natif, pas besoin d'une table séparée pour
  un besoin de filtrage simple) pour les labels libres.
- Le choix d'environnement à la création d'un déploiement/recette devient un
  paramètre explicite de la requête (`POST /api/v1/projects/:name/deploy` ou
  équivalent existant), pas déduit implicitement de `recette_name`.
- **Lien avec la question ouverte n°1 (rétention)** : ce ticket structure
  seulement la donnée `environment`/`tags`. La politique de rétention
  (auto-destroy après N heures pour les recettes) reste un chantier séparé
  — mais elle doit désormais lire ce champ `environment` structuré plutôt
  que de deviner via `recette_name`. Ne pas coder de politique de rétention
  dans ce ticket, seulement préparer le terrain.
- Frontend : filtre par environnement sur la page détail projet (B2/B3) et
  sur la liste des déploiements existante (`Deployments.vue`).
- Provisioning/lecture uniquement, aucune exécution applicative nouvelle —
  cohérent avec la décision n°3.

---

## EPIC E — Méthodologie d'administration de la plateforme (gitflow, triggers)

### E1 🟡 Comment un admin DevOps configure Symphony selon le mode de travail de son entreprise

Ce n'est pas une feature au sens où le reste du backlog l'entend — c'est une
question de recherche produit encore ouverte, que le fondateur pose
lui-même sans y répondre. Elle touche potentiellement au contrat
`CIProvider` (`TriggerPipeline`, `SetupPipeline`) et aux templates de
pipeline (EPIC F), donc rien ne doit être conçu avant d'avoir une réponse.

**Action requise** : le fondateur doit d'abord lister *quelles* méthodologies
(gitflow ? trunk-based ? déclenchement manuel vs automatique sur push/MR ?)
Symphony prétend supporter au MVP. Sans cette liste, impossible de
distinguer "ce qui doit être configurable par golden path" de "ce qui reste
une convention imposée par Symphony". Voir question en fin de document.

---

## EPIC F — Templating plus granulaire (outils de test/build variables)

### F1 🟢 Granularité des outils dans les pipelines (tests, build d'image) — via variables de golden-path.yaml

**Décision du fondateur (2026-07-24)** : variables dans `golden-path.yaml`
plutôt que golden paths distincts ou briques composables — reste le plus
simple à greffer sur le loader actuel, cohérent avec le fait que
`internal/templates/`/`internal/catalog/` est la zone la moins mature du
projet (prudence de conception requise, voir skill
`declarative-scaffolding`).

**Constat code** : `golden-path.yaml` actuel (ex: `config/golden-paths/go-rest-api/golden-path.yaml`)
n'expose que `metadata` + `spec.language/type/default_port`. `ci/pipeline.yml`
est un template statique par golden path, aucune variable de choix d'outil.

**Spec** :
- Étendre le schéma `spec` de `golden-path.yaml` avec des clés d'outillage,
  ex. `spec.test_tool` (`pytest`, `go test`, `jest`...), `spec.build_tool`
  (`docker build`, `buildkit`...) — commencer par les axes que le fondateur
  a explicitement cités (tests, build d'image), ne pas anticiper d'autres
  axes non demandés (YAGNI).
- `internal/templates/loader.go` : ces clés deviennent des variables
  `text/template` disponibles dans `ci/pipeline.yml`, au même titre que les
  variables déjà exposées (`RenderCI()`), pas une branche conditionnelle en
  Go — le loader ne doit pas savoir ce qu'est "pytest", il substitue une
  variable.
- Valeur par défaut par langage (ex: `go test` pour `language: go`) pour ne
  pas casser les golden paths existants qui ne définissent pas encore ces
  clés.
- **Reste couplé à la question ouverte n°3 résiduelle** (validation de
  schéma golden-path.yaml, comportement si une clé d'outil référence un
  fragment de template manquant) — traiter la validation dans le même
  chantier plutôt que de la reporter indéfiniment : au minimum, rejeter au
  chargement un `golden-path.yaml` qui référence une variable sans valeur
  par défaut connue, avec un message d'erreur explicite (pas un échec
  silencieux au moment du rendu).

---

## Résumé priorisation (mis à jour après arbitrages du 2026-07-24)

| Item | Statut | Effort estimé | Bloque quoi |
|---|---|---|---|
| B1 — accueil = projets | 🟢 | XS (routing) | — |
| B2 — page détail projet | 🟢 | S | B3 |
| B3 — jobs/images/branches | 🟢 | M (extension SCMProvider) | — |
| A1 — seed démo multi-stades | 🟢 | S | — |
| A2 — doc démo pipeline complet | 🟢 | XS (doc) | — |
| C1 — mode super-admin (bascule UI) | 🟢 | XS | — |
| D1 — environment + tags | 🟢 | S (migration additive) | politique rétention (Q ouverte #1, hors scope de ce ticket) |
| F1 — templating granulaire (variables) | 🟢 | M | Q ouverte #3 résiduelle traitée dans le même ticket |
| C2 — multi-provider (design seul, pas de code) | 🟢 (livrable = doc) | S | A4 reste différé |
| E1 — méthodologie admin (gitflow/triggers) | 🟡 | recherche produit — réponse libre attendue | F1 v2 potentiellement |
| A3 — provider en démo (instance vs type) | 🟡 | dépend clarification | — |
| B4 — graphiques/métriques | 🟡 | dépend liste métriques | — |
| A4 — import projet existant | 🔴 | — | dépend d'un futur C2 "implémenter" |

Ordre d'attaque recommandé : **B1 → B2 → B3 → A1 → A2 → C1 → D1 → F1 → C2**
(tout actionnable dès maintenant). E1, A3 et B4 restent en attente d'une
réponse libre du fondateur (voir ci-dessous) — pas de spec tant que ces
listes ne sont pas données, conformément à la règle CLAUDE.md sur les
zones non tranchées.

---

## Encore à préciser (réponse libre, pas un choix parmi des options)

Ces trois points ne se prêtent pas à un choix multiple — ils demandent une
liste ou une clarification en texte libre de ta part avant qu'une spec
puisse être écrite :

1. **E1 — méthodologies à supporter au MVP** : quelles stratégies de
   branching/déclenchement Symphony doit-il connaître (gitflow ? trunk-based
   ? déclenchement manuel uniquement vs auto sur push/MR) ? Sans cette
   liste, impossible de savoir ce qui doit devenir un paramètre de golden
   path vs rester une convention imposée par Symphony.
2. **A3 — "l'utilisateur enregistre un provider" en démo** : confirmer que
   ça veut dire enregistrer une *instance* d'un type déjà compilé (ex:
   pointer vers un second GitLab via le wizard) et pas un *type* de provider
   inconnu de Symphony (ce qui contredirait la décision n°2 sur les drivers
   compilés).
3. **B4 — métriques à afficher sur la page détail projet** : quelle liste
   fermée de métriques (ex: CPU/mémoire des instances, taux d'erreur,
   latence...) ? Sans liste fermée, risque de reconstruire un mini-Grafana
   alors que le produit promet justement un lien externe vers Prometheus
   pour aller plus loin.
