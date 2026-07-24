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

### A3 🟢 L'utilisateur enregistre lui-même un provider dans la démo — résolu, fusionné dans A2

**Réponse (2026-07-24)** : ça ne peut être qu'une **instance** d'un type déjà
compilé — enregistrer un *type* inconnu contredirait la décision
d'architecture n°2 (drivers compilés, jamais de plugin dynamique runtime), et
le multi-provider par catégorie reste de toute façon au stade design-only
(C2), donc "enregistrer un second provider de la même catégorie" n'est même
pas buildable dans ce cycle.

En relisant le retour à la lumière de ça, la lecture la plus utile n'est pas
"ajouter un 2ᵉ provider" mais : **faire dérouler le wizard de config par
l'utilisateur lui-même pendant la démo**, plutôt que de le laisser
pré-configuré silencieusement par le script d'infra. C'est exactement le
principe déjà documenté dans la section Sécurité de CLAUDE.md ("si
`SYMPHONY_DEV_MODE` dispense aussi de la config, le wizard n'est jamais
réellement exercé en dev/démo — bug déjà rencontré une fois").

**Spec** : ce point n'est plus un item séparé, il devient une exigence
explicite de **A2** — le script de démo (infra) ne doit préconfigurer que ce
qui est strictement nécessaire pour que l'app démarre (base Postgres, GitLab
local up), et `DEMO.md` doit faire passer l'utilisateur par `/setup` pour
saisir lui-même au moins un provider (ex: token GitLab), avant le seed de
projets (A1). Aucun changement de code requis au-delà de A1/A2, seulement
l'ordre des étapes dans `DEMO.md`.

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

### B4 🟢 Panneau d'état opérationnel (pas des graphiques) sur la page détail — résolu

**Réponse (2026-07-24)** : ne pas construire de graphiques/métriques
numériques (CPU, latence, etc.) pour ce cycle. Deux raisons cumulatives : le
fondateur a lui-même laissé le point flou, et CLAUDE.md est explicite — le
monitoring poussé reste hors MVP (lien externe Prometheus, déjà implémenté
via `monitoring_url` dans `golden-path.yaml` et affiché dans `Projects.vue`).
Construire des graphiques inline reviendrait à reconstruire un mini-Grafana,
exactement ce que le produit promet de ne pas faire.

**Proposition** : à la place, un **panneau de statut opérationnel** — pas des
métriques au sens Prometheus, mais des données que Symphony possède déjà
nativement, donc sans risque d'intégration ni de nouvelle dépendance :
- **État des déploiements par environnement** (recette/préprod/prod, cf. D1)
  — running/stopped/failed, via `DeployProvider.ListContainers()`
  (`internal/providers/interfaces.go:82-87`), déjà en lecture seule.
- **Taux de succès des N derniers pipelines** (ex: 10 derniers) — dérivé de
  la table `pipelines` déjà en base, aucun nouveau provider requis.
- **Dernière image poussée par environnement** — via
  `RegistryProvider.ListImages()` (déjà existant), croisée avec B3.
- Le lien vers `monitoring_url` reste le point d'entrée pour tout ce qui est
  métriques réelles (CPU, mémoire, latence) — Symphony ne duplique jamais ce
  que Prometheus fait déjà.

**Ce qui reste explicitement hors scope** : toute jauge/graphique temporel,
tout appel direct à l'API Prometheus depuis Symphony. Si un besoin réel
apparaît plus tard, ce serait une décision d'architecture à part entière (un
nouveau type de provider "Monitoring" ?) à passer devant
`architecture-guardian`, pas une extension silencieuse de ce panneau.

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

### E1 🟢 Comment un admin DevOps configure Symphony selon le mode de travail de son entreprise — résolu

**Réponse (2026-07-24)** : en relisant `CIProvider` (`internal/providers/interfaces.go:44-49`),
Symphony n'a **pas besoin de connaître** gitflow, trunk-based ou toute autre
convention comme concept de premier ordre. `TriggerPipeline(projectPath, ref
string, ...)` prend déjà un `ref` (branche/tag) générique, et
`GetPipelineStatus` ne présuppose aucune structure de branches particulière.
Le comportement différencié par branche (ex: `develop` déclenche un déploiement
recette auto, `main` non ; `release/*` a des règles différentes) est déjà
nativement exprimable dans `ci/pipeline.yml` via la syntaxe `rules:`/`only:`/
`except:` de l'outil CI cible (GitLab CI aujourd'hui) — **sans aucun code
Symphony**, cohérent avec la décision n°5 (templates déclaratifs, admin
n'écrit jamais de Go) et la décision n°3 (exécution toujours déléguée à
l'outil CI).

**Ce que Symphony garantit, indépendamment de la méthodologie choisie par
l'entreprise** :
- le déclenchement manuel depuis l'UI fonctionne pour n'importe quel `ref`
  (branche/tag), pas seulement la branche par défaut ;
- le suivi de statut (webhook + réconciliation 30s) fonctionne quel que soit
  le branching model, puisqu'il est indexé sur `pipeline_id`, pas sur un nom
  de branche particulier ;
- le template `ci/pipeline.yml` de chaque golden path reste éditable par
  l'admin pour y ajouter ses propres règles par branche, sans que Symphony
  n'impose une convention.

**Conséquence pour le backlog** : pas de ticket de développement séparé.
Documenter simplement, dans le futur guide admin (`config/golden-paths/*/README`
ou équivalent), que la stratégie de branching est **la responsabilité de
l'admin dans le template CI**, pas une fonctionnalité de Symphony. Si un
besoin concret et bloquant apparaît plus tard (ex: un vrai client qui ne peut
pas exprimer sa règle en `rules:` GitLab CI), rouvrir ce point avec un cas
précis plutôt que dans l'abstrait.

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

Tous les points sont désormais résolus ou explicitement différés — plus
aucun item 🟡 en attente de clarification.

| Item | Statut | Effort estimé | Bloque quoi |
|---|---|---|---|
| B1 — accueil = projets | 🟢 | XS (routing) | — |
| B2 — page détail projet | 🟢 | S | B3 |
| B3 — jobs/images/branches | 🟢 | M (extension SCMProvider) | — |
| A1 — seed démo multi-stades | 🟢 | S | — |
| A2 — doc démo pipeline complet (inclut A3) | 🟢 | XS (doc) | — |
| C1 — mode super-admin (bascule UI) | 🟢 | XS | — |
| D1 — environment + tags | 🟢 | S (migration additive) | politique rétention (Q ouverte #1, hors scope de ce ticket) |
| F1 — templating granulaire (variables) | 🟢 | M | Q ouverte #3 résiduelle traitée dans le même ticket |
| C2 — multi-provider (design seul, pas de code) | 🟢 (livrable = doc) | S | A4 reste différé |
| E1 — méthodologie admin (gitflow/triggers) | 🟢 (pas de dev requis, cf. réponse) | — (documentation seule) | — |
| B4 — panneau de statut opérationnel (pas de graphiques) | 🟢 | S | — |
| A4 — import projet existant | 🔴 | — | dépend d'un futur C2 "implémenter" |

Ordre d'attaque recommandé : **B1 → B2 → B3 → A1 → A2 → C1 → D1 → F1 → B4 →
C2**, E1 se limitant à une note dans la doc admin (pas de développement).
A4 reste le seul item différé, dans l'attente d'une future décision
d'implémenter réellement le multi-provider (C2).

---

## Décisions prises le 2026-07-24 (log)

Pour traçabilité — ces réponses ont été proposées par Claude en rôle de CTO
(cf. `.claude/CLAUDE.md`, section "comment travailler sur ce projet") et
valent décision de travail jusqu'à contre-ordre du fondateur :

1. **E1 — méthodologies à supporter au MVP** : Symphony ne modélise aucune
   méthodologie (gitflow, trunk-based...) comme concept propre. Le contrat
   `CIProvider` est déjà générique sur `ref` ; la logique par branche se
   pose dans `ci/pipeline.yml` via la syntaxe native de l'outil CI, pas dans
   Symphony. Aucun développement requis — juste une note dans la doc admin.
2. **A3 — "l'utilisateur enregistre un provider" en démo** : confirmé comme
   instance d'un type déjà compilé, et reformulé en exigence pour A2 — le
   script de démo laisse l'utilisateur passer par `/setup` lui-même au lieu
   de tout préconfigurer silencieusement.
3. **B4 — métriques sur la page détail projet** : pas de graphiques
   temporels pour ce cycle. À la place, un panneau de statut opérationnel
   basé sur des données déjà possédées par Symphony (état des déploiements,
   taux de succès des derniers pipelines, dernière image poussée), le lien
   `monitoring_url` restant le point d'entrée pour les vraies métriques.
