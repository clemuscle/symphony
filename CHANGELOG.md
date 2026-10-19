# Changelog

Les versions suivent [ROADMAP.md](ROADMAP.md). Chaque version publiée a son tag `vX.Y` et son
image `ghcr.io/clemuscle/symphony:X.Y`.

## v0.15 — L'équipe et la gouvernance

### Ajouté
- **Rôle `lead`** (viewer < developer < lead < admin), donné par un groupe de rôle
  (`auth.roles.lead`) : un lead agit comme un developer sur les projets de ses équipes et
  approuve leurs déploiements.
- **Approbation par environnement** (`environments[].approval: lead`) : un déploiement,
  une promotion ou un retour arrière demandé par un developer attend l'accord d'un lead de
  l'équipe du projet ou d'un admin (24 h au plus) ; le demandeur peut retirer sa demande.
  Un lead ou un admin qui demande lui-même passe sans attendre. Nouvelle étape
  « Approbation » des tâches de déploiement ; routes `POST /api/v1/tasks/{id}/approve` et
  `…/reject` (avec commentaire), `GET /api/v1/approvals` ; décisions dans l'audit
  (`deploy_approved`, `deploy_rejected`).
- **Cloche des leads** : les déploiements de leurs équipes qui attendent leur accord, en
  direct, et une section « À approuver » en tête de l'Activité, avec Approuver / Refuser.
- **Notifications** Slack et Microsoft Teams : providers `notifications` (`slack`,
  `teams`, webhook entrant en `secret://`) et section `notifications` (canaux par équipe,
  par défaut, adresse de Symphony pour les liens). Approbation demandée, accordée ou
  refusée, déploiement réussi ou échoué, échec d'une autre tâche du projet.
- **Historique des déploiements** par environnement (origine, demandeur, approbateur,
  tâche, statut) : `GET /api/v1/releases`, et sur la page du projet.
- **Matrice versions × environnements** : nouvelle page **Versions** (quelle version
  tourne où, ce qui attend une approbation ou une promotion, ce qui a échoué).
- **Retour arrière** en un clic (`POST /api/v1/projects/{nom}/rollback`) : vers la
  version précédente d'un environnement ou une version de son historique, y compris en
  production protégée (l'approbation s'applique).
- Démo : lina, lead de `team-payments` ; production avec `approval: lead` ; canal Slack
  avec `SLACK_WEBHOOK_URL`.

### Modifié
- La table `deployments` devient l'historique des versions (migration 007) ; arrêter un
  déploiement ne change plus le statut de ses versions précédentes.
- `symphony validate` remplace les secrets par une URL factice (les clés qui attendent
  une URL, comme `webhook_url`, sont vérifiées hors ligne).


### Ajouté
- **Fiches Backstage** : `catalog-info.yaml` est un Component `backstage.io/v1alpha1`. Ce
  que Backstage a vient de ses champs (`spec.owner`, `lifecycle`, `type`, `dependsOn`,
  `metadata.links`…), les valeurs de Symphony d'annotations `symphony.io/…` (tier,
  langage, port, contact de l'équipe), les structures de champs en plus dans `spec`
  (environnements, actions, SLO, manifests, infra). Un fichier à plusieurs entités est
  accepté : Symphony lit le Component du projet. Les nouveaux projets reçoivent une fiche
  Backstage. Fiche de référence : `documentation/reference/catalog-info.yaml`, vérifiée
  par les tests.
- **Découverte des services existants** (`catalogue.discovery`) : des groupes GitLab
  (sous-groupes compris) ou organisations GitHub scannés à intervalle (`interval`, 5 min
  par défaut) ou à la demande (« Relire Git ») ; chaque repo qui a une fiche devient un
  projet, au nom de sa fiche, à l'équipe de son `spec.owner` (si l'annuaire la connaît),
  avec le CI et le registry de sa source, motifs `exclude`. Symphony n'écrit rien dans ces
  repos. Conflits de nom, fiches illisibles, équipes inconnues et projets orphelins (que
  leur source ne donne plus) se lisent dans la page Administration ; `symphony validate`
  vérifie la section.
- Retirer un projet découvert supprime ses déploiements, jamais son repo ni ses images ;
  refusé (409) tant que sa source le donne encore.
- Méthode SCM `ListGroupRepos` (GitLab, GitHub : organisation ou compte).
- Démo : groupe `services` et `invoice-service`, poussé sans Symphony avec une fiche
  Backstage (Component et API), découvert puis déployé en staging.
- Interface : de vraies icônes à la place des émojis, logos des outils (langages, GitLab,
  GitHub, Docker, Kubernetes…, [Simple Icons](https://simpleicons.org), CC0) et
  pictogrammes ([Lucide](https://lucide.dev), ISC), par un composant `AppIcon` ; le
  runtime d'un déploiement est reconnu par son id plutôt que par ceux de la démo.

### Modifié
- L'ancienne fiche (`symphony/v1`, `kind: Service`) est lue jusqu'à la v1.0 et signalée
  dans Administration.
- La branche d'un projet est celle par défaut de son repo (`projects.branch`, migration
  006) : fiche, manifests, pipelines et actions ne supposent plus `main`.

### Sécurité
- Une action pipeline tourne sur le repo du projet connu de Symphony, et non sur celui que
  donne sa fiche (`spec.repo`) : un repo pouvait lancer les pipelines d'un autre avec le
  jeton de Symphony. Le CI de l'action doit tourner sur la plateforme du SCM du projet.

## v0.13 — L'infra avec l'appli

### Ajouté
- **Infra d'une stack** (`spec.infra`) : un dossier de manifests d'opérateurs Kubernetes
  et les variables que l'appli lit dans les secrets qu'ils créent (`env`). Reprise dans la
  fiche du projet ; le runtime argocd l'applique en GitOps avec le service, une par
  environnement (objets nommés `<déploiement>-<nom>`, jamais élagués par Argo CD), et
  elle part avec le projet. Création refusée si un environnement du projet n'est pas
  sur Kubernetes ; le runtime docker refuse un déploiement avec infra.
- **Santé de l'infra** : chaque déploiement porte ses objets d'infra et leur état Argo CD
  (`infra` dans l'API), affichés par environnement sur la page du projet.
- **Option PostgreSQL** de la stack officielle `api-rest-python` (CloudNativePG, taille en
  paramètre) : `DATABASE_URL` lue dans le secret de la base, route `/db`.
- Démo : opérateur CloudNativePG dans k3s, stack `api-rest-python-k8s` ; `auth-service`
  est créé avec sa base.
- CI : `TestCNPGOnCluster` sur kind avec l'opérateur (base créée, jointe avec l'URI de
  son secret, supprimée avec ses volumes).
- L'API expose les fichiers sous condition d'une stack (`files`), pour que le formulaire
  dise si l'infra sera créée.

### Corrigé
- Runtime argocd : l'image d'un déploiement est celle que Symphony a déployée
  (annotation `symphony/image` de l'Application), et non la première du résumé d'Argo
  CD, qui liste aussi celles de l'infra : une promotion pouvait déployer l'image de
  l'opérateur de la base.
- Catalogue : le résumé n'annonce plus le chart Helm ou la base Kustomize d'une stack
  quand l'option qui les crée est décochée.

## v0.12 — Stacks paramétrables

### Ajouté
- **Paramètres de stack** (`spec.parameters`) : `name`, `title`, `description`, `type`
  (string, number, boolean, choice), `choices`, `default`, `required`, `when` (champ
  conditionnel), `locked` (valeur fixée par l'équipe plateforme). Traduits en JSON Schema
  (servi par l'API, champ `schema`), qui valide les valeurs à la création : erreurs en
  français, paramètre inconnu refusé. Le Catalogue les rend en formulaire ; les valeurs
  vont dans le squelette et la CI (`{{params.<nom>}}`) et restent avec le projet
  (`stack_parameters`, affichés sur sa page).
- **Fichiers sous condition** (`spec.files[].when`) : un fichier ou dossier du squelette
  n'est créé que si les paramètres le demandent.
- **Manifests Kubernetes fournis par la stack** (`spec.kubernetes` : `helm` ou
  `kustomize`, repris dans la fiche du projet) : le runtime argocd déploie le chart Helm
  (avec un `values.yaml` de Symphony : image, réplicas, variables, secrets, Ingress) ou la
  base Kustomize (préfixe, image `app`, variables, Ingress) du repo du service, au lieu
  de générer ses manifests. Arrêter et relancer passent par les mêmes fichiers ; les
  fichiers d'un déploiement précédent qui ne servent plus sont retirés du repo de
  déploiement. Contrat dans `documentation/reference/stack.yaml`.
- **Catalogue officiel** (`stacks/`, embarqué dans le binaire) : `api-rest-go` (chart
  Helm en option), `api-rest-python` (base Kustomize en option), `api-rest-node`,
  `worker-python` (un vrai worker, intervalle en paramètre). Sans provider désigné, avec
  une CI par forge (`spec.ci_templates`), ils suivent les providers par défaut, GitLab ou
  GitHub. Exposés par `stacks.official` (`["*"]` ou une liste) ; une stack du repo du
  même nom les remplace.
- Un fichier du squelette en `.tmpl` est créé sans ce suffixe (`go.mod.tmpl` → `go.mod`).

### Modifié
- **« Golden path » devient « stack »**, dans l'interface, la doc, l'API et le format :
  `stack.yaml` avec `kind: Stack`, section `stacks` de la configuration (défaut du dossier :
  `stacks`), `GET /api/v1/stacks`, champs `stack` et `parameters` à la création, `stack`
  sur un projet (colonne renommée par la migration 005), clé `stacks` de
  `/api/v1/admin/status`, métrique `symphony_stacks`, `symphony validate -stacks`.
  L'ancien format (`template.yaml`, `kind: GoldenPath`, section `templates`, défaut
  `root/symphony-templates`) est encore lu jusqu'à la v1.0, et signalé dans
  Administration (nouveaux avertissements : `warnings` de la configuration et des stacks).
- Quickstart : les stacks officielles, sans rien pousser sur la forge
  (`push-templates.sh` retiré) ; `STACKS_REPO` facultatif pour celles de l'entreprise.
- Démo : `platform/symphony-stacks` ne garde que ses variantes (`api-rest-go-k8s` avec
  son chart Helm verrouillé, `*-github`) ; le reste vient du catalogue officiel.
- Runtime argocd : les journaux visent le Deployment de l'Application (le nom d'un chart
  ou d'une base peut différer de celui du déploiement).

## v0.11 — L'admin y voit clair

### Ajouté
- **Page Administration** (admin) : configuration en place (source, empreinte, dernière
  version refusée et sa raison, source ou secrets illisibles), providers et leur état,
  golden paths chargés et refusés avec la raison, fiches catalogue illisibles et projets
  sans fiche, journal d'audit. Un résumé dit ce qui demande attention.
- **Vérification des providers** : chaque provider a une méthode `Check` en lecture seule
  (utilisateur du jeton GitLab ou GitHub, version de Docker et d'Argo CD, jeton Vault,
  découverte OIDC et annuaire Keycloak), lancée en parallèle à l'ouverture de la page ;
  le dernier appel de chaque provider est aussi retenu.
- API : `GET /api/v1/admin/status` et `GET /api/v1/admin/providers/check` (admin).
- **`symphony validate -config … -templates …`** : configuration et golden paths vérifiés
  hors ligne, comme au chargement (schéma, références de secrets, construction des
  providers, environnements, auth, webhooks, golden paths contre ces providers), sans
  lire aucun secret ; toutes les erreurs d'un coup, code de sortie 1.
- **Configuration de référence** commentée (`documentation/reference/`) : chaque clé de
  chaque type de provider, le bootstrap et un golden path ; les tests échouent si une clé
  du schéma n'y est pas.
- **Quickstart** (`quickstart/`) : Symphony, PostgreSQL et un proxy de socket Docker en
  Compose, branchés sur gitlab.com, une GitLab auto-hébergée ou GitHub ;
  `push-templates.sh` pousse les golden paths d'exemple qui conviennent (`--dry-run`).
- **Licence Apache 2.0.**

### Modifié
- CI GitLab des golden paths d'exemple : sur gitlab.com, ou avec `DOCKER_DIND=true` dans
  les variables du groupe, l'image se construit avec un service `docker:dind` (job
  `build-dind`) ; ailleurs, avec le socket du runner comme avant. `docker login` lit le
  mot de passe sur l'entrée standard.
- Roadmap réécrite d'après [documentation/vision.md](documentation/vision.md) (v0.11 à
  v0.17, puis v1.0).

## v0.10 — Finitions

### Modifié
- **Activité** : un déploiement dit son environnement, une promotion d'où elle part
  (« Promotion staging → production ») ; résultat `target` des tâches de déploiement.
- **Cycle de vie** d'un projet affiché en mots qui ne se confondent pas avec les
  environnements (« en service », « en rodage »…), avec son infobulle.
- **API** : `provider` devient facultatif pour les logs, la relance et l'arrêt d'un
  déploiement ; sans lui, Symphony cherche le déploiement dans chaque runtime (400 s'il
  existe dans plusieurs).

### Corrigé
- Heures des tâches au format français, quelle que soit la langue du navigateur.
- Plus d'erreur en console au premier chargement : le 401 attendu avant la connexion
  laisse la page partir vers Keycloak sans rejeter la requête.
- Sans `provider`, logs, relance et arrêt d'un déploiement hors du runtime par défaut
  répondaient 404 « introuvable ».
- Squelettes Go des golden paths formatés avec `gofmt`, désormais vérifiés par la CI.
- « pas de .env trouvé » logué en information, plus en avertissement (cas normal en
  conteneur).
- Taille de l'image dans la documentation : ~15 Mo (6 Mo compressée), pas ~10 Mo.
- Migrations sérialisées par un verrou consultatif PostgreSQL : deux instances (ou deux
  paquets de tests) démarrées ensemble sur une base neuve créaient les mêmes tables en
  même temps et l'une échouait.

## v0.9 — La vitrine

### Ajouté
- **Environnements et promotion** : `environments` dans `symphony.yaml` (ordre de
  promotion, runtime, `protected`) ; un golden path peut changer le runtime d'un
  environnement. Un déploiement s'appelle `<projet>-<environnement>` ; un environnement
  protégé ne reçoit que des promotions (`POST /api/v1/projects/{nom}/promote`) de l'image
  saine de l'environnement précédent, y compris d'un runtime à l'autre. L'UI montre chaque
  environnement du projet et propose la promotion.
- **Variables et secrets des applications** : déclarés par environnement dans la fiche
  (`spec.environments[].env`), valeurs ou `secret://vault/apps/<projet>/…` (un projet ne lit
  que ses secrets, jamais l'environnement de Symphony). Docker : résolus au déploiement,
  jamais enregistrés. Argo CD : `ExternalSecret` (External Secrets Operator), aucune valeur
  dans Git (`secret_store`). Démo : External Secrets installé dans k3s et branché sur Vault ;
  `payment-api` lit son mot de passe de base dans Vault ; projets déployés en staging puis
  promus en production.
- **Webhooks GitHub** : les repos GitHub sont abonnés aux événements `workflow_run`
  (`/webhooks/github`, signature HMAC-SHA256 vérifiée en temps constant) ; les tâches
  avancent sans attendre le sondage.
- **Réabonnement après rotation** : quand l'URL ou le secret des webhooks change (par
  exemple dans Vault), les repos de tous les projets sont réabonnés avec le nouveau
  secret ; GitLab et GitHub mettent à jour un webhook existant au lieu de l'ignorer.
- **Mode visite** : la vraie interface, publiée sur GitHub Pages
  (https://clemuscle.github.io/symphony/), servie par des données capturées sur la démo
  (`frontend/src/visit`), en lecture seule ; aucun serveur, essai en une minute.
- **Nouveau GIF** : création d'un projet, déploiement en staging, promotion en
  production sur Kubernetes.
- **Tests d'intégration** : le parcours complet par l'API (création, pipeline, fiche,
  staging, refus d'un déploiement direct en production, promotion, action Day-2,
  suppression) tourne en CI sur une vraie base, avec des providers simulés
  (`internal/provider/fake`) ; les droits par équipe sont testés route par route. Couverture
  des paquets de l'API : de 10 % à 76 %.
- **API décrite en OpenAPI 3.1** (`internal/api/openapi.yaml`, servie sur
  `/api/openapi.yaml`) : chaque route, son rôle minimum (`x-symphony-role`), ses réponses.
  `TestOpenAPICoversRoutes` vérifie qu'elle couvre exactement les routes du serveur ; la
  CI la valide (Redocly).
- Golden path `api-rest-python-github` (FastAPI, GitHub Actions, GHCR).
- **Front en TypeScript strict** (`vue-tsc`), ESLint (règles Vue et TypeScript), tests
  Vitest (logique des projets, droits, tâches, composants) ; la CI les lance avec
  `npm audit`. `Projects.vue` (434 lignes) découpé en `ProjectCard`, `ProjectPanel` et
  `DayTwoAction`, logique pure dans `src/lib/`.
- Le runtime `docker` accepte `socket: tcp://<hôte:port>` (proxy de socket).

### Sécurité
- **Image non-root** (`USER 65532`). En démo, Symphony ne monte plus le socket Docker :
  il passe par un proxy (`tecnativa/docker-socket-proxy`) qui n'ouvre que les
  conteneurs et les images.
- Identifiants de requête courts (16 caractères hexadécimaux) ; ceux de chi contenaient
  le nom de la machine. Un `X-Request-ID` fourni et sûr est repris.
- `/metrics` ne répond plus qu'en GET (il acceptait toutes les méthodes).
- Une image en `latest` (tag mobile) ne peut pas être promue : la production pourrait
  changer sans nouvelle promotion. Le formulaire de déploiement propose par défaut le
  dernier tag immuable.

### Modifié
- `SCMProvider.CreateWebhook` devient `EnsureWebhook` (crée ou met à jour le secret).
- `POST /api/v1/deployments` ne prend plus de variables (`env_vars`) : elles viennent de la
  fiche ; il prend `environment`. `RuntimeProvider.Remove(projet)` supprime tous ses
  environnements ; les déploiements portent `project` et `environment`.
- Dépendances du front à jour : les failles connues d'axios, vue et de leurs dépendances
  sont corrigées (`npm audit` vide) ; restes du gabarit Vite supprimés.

## v0.8 — Prêt pour la prod

### Ajouté
- **Chart Helm** (`deploy/helm/symphony`) : Symphony s'installe sur Kubernetes, en
  non-root, système de fichiers en lecture seule, sans jeton Kubernetes ; bootstrap en
  ConfigMap (pod relancé s'il change), secrets dans un Secret existant
  (`secret://env/…`), Ingress en option. La CI l'installe sur un cluster kind et vérifie
  santé, API et UI.
- Au démarrage, Symphony attend PostgreSQL une minute au lieu de s'arrêter (base et
  appli démarrées ensemble).
- **Droits par équipe** : un projet appartient à une équipe de l'annuaire (`projects.team`,
  migration 004) ; seuls ses membres developers et les admins le déploient, l'arrêtent, le
  relancent, lisent ses logs, relancent sa pipeline et lancent ses actions. À la création,
  un developer choisit parmi ses équipes. `GET /api/v1/me` renvoie les équipes ; les
  groupes de rôles ne sont plus proposés comme équipes. L'UI masque ce que l'équipe ne
  permet pas.

### Sécurité
- `POST /api/v1/pipelines/trigger` acceptait n'importe quel repo et n'importe quelles
  variables CI, ce qui contournait la validation des entrées des actions : il prend
  désormais un nom de projet et relance sa pipeline sans variables.

### Modifié
- **Configuration stricte** : `symphony.yaml` est décodé sans tolérance (clé inconnue =
  erreur) et chaque type de provider déclare ses clés autorisées et requises ; une faute
  de frappe, une clé requise absente, un type inconnu ou un id en double font refuser la
  configuration, avec toutes les erreurs d'un coup. Un provider impossible à construire
  fait refuser la configuration au lieu d'être ignoré.
- **Erreurs internes masquées** : le client reçoit l'opération en échec et l'identifiant
  de requête ; le détail (SQL, réponse brute d'un provider) reste dans les logs.
- Go 1.27 (`go.mod`, CI, image).
- Un seul nom dans l'UI : « Internal Developer Platform ».

### Retiré (compatibilité pré-1.0)
- `token_env` et les autres clés `*_env` : `secret://env/<VARIABLE>` à la place.
- Replis implicites sur `GITLAB_TOKEN`, `GITHUB_TOKEN`, `<scm_ref>_TOKEN`,
  `ARGOCD_TOKEN` et `WEBHOOK_SECRET` : le jeton vient de `token` ou du SCM `scm_ref`,
  le secret des webhooks de `webhooks.secret`.
- Ancien format de `auth` (`issuer`, `client_id`…) : déclarer un provider `identity`.
- `catalogue.repo`, `catalogue.branch`, `catalogue.path`.

### Corrigé
- Les réponses JSON illisibles des providers (GitLab, GitHub, GitLab CI, Docker) sont des
  erreurs, au lieu de valeurs vides.

## v0.7 — Multi-plateforme et robustesse

### Ajouté
- **Secrets en providers** (`providers.secrets`) : types `vault` (HashiCorp Vault ou
  OpenBao, KV v2 ou v1, jeton ou AppRole renouvelé à expiration) et `env` (toujours
  présent). Toute valeur de configuration d'un provider, et le secret des webhooks, peut
  être une référence `secret://<provider>/<chemin>#<clé>`. Les références sont résolues
  dans une copie de la configuration, tout ou rien, et relues à chaque cycle : un jeton
  changé dans Vault est appliqué sans redémarrage. Le bootstrap local peut déclarer ses
  propres secrets (jeton de `config_source` dans Vault).
- **Identité en providers** (`providers.identity`) : `oidc` (tout fournisseur OIDC) et
  `keycloak` (issuer et découverte déduits de l'URL et du realm, et **annuaire** par l'API
  d'administration). `GET /api/v1/identity/groups` ; le formulaire de création propose
  les groupes comme équipe propriétaire.
- **Golden path GitHub de bout en bout** (`api-rest-go-github` dans la démo) : repo GitHub,
  workflow GitHub Actions (tests, image sur GHCR, entrées `SYMPHONY_*` de
  `workflow_dispatch`), déploiement Docker. Nouveau registry `ghcr` (tags et identifiants
  de pull par l'API des packages).
- Les registries ont une méthode `DeleteImages` : supprimer un projet GitHub supprime aussi
  son package GHCR.
- L'image de Symphony a un `HEALTHCHECK` (`symphony -healthcheck`).
- Démo : **Vault** (mode dev) rempli par `up.sh` ; Symphony n'a plus que son AppRole dans
  son environnement (jetons GitLab, Argo CD et GitHub, secret des webhooks, client de
  l'annuaire dans Vault). Groupes d'équipes dans le realm Keycloak (`teams/team-*`) et
  client de service `symphony-directory`. `GITHUB_TOKEN=… ./demo/up.sh` ajoute GitHub.
  `VAULT_PORT` (8200).

### Modifié
- **Bascule Docker** : le nouveau conteneur remplace l'ancien quand il est `healthy`
  (`HEALTHCHECK` de l'image, `health_timeout` du runtime, 2 min par défaut) ; sans
  healthcheck, après 3 s comme avant. L'erreur contient la sortie du healthcheck. Les
  golden paths de la démo sondent toutes les 10 s.
- La section `auth` ne porte plus que `identity` et `roles`. L'ancien format (`issuer`,
  `client_id`…) est converti en provider `oidc` avec un avertissement.
- `webhooks.secret` (valeur ou référence) à côté de `webhooks.secret_env`.
- GitHub : le squelette est poussé en **un seul commit** (API Git) au lieu d'un commit par
  fichier (et d'un workflow par fichier) ; le repo est créé initialisé, dans `Namespace`
  ou `org` s'ils sont renseignés.
- GitHub Actions : statuts ramenés à ceux de Symphony (`failure` → `failed`,
  `cancelled` → `canceled`, `in_progress` → `running`…), id du run lancé retrouvé (une
  action Day-2 attendait un run d'id `pending`, introuvable).
- CI et registry : le jeton peut venir du SCM `scm_ref` de la configuration (une seule
  référence au secret) ; GitHub Actions et GHCR se rabattent sur `GITHUB_TOKEN`.
- `GET /api/v1/providers` liste aussi les providers `secrets` et `identity`.

### Corrigé
- Golden paths Go : `/app -healthcheck` (le `HEALTHCHECK` de leur image) lançait un
  second serveur au lieu d'interroger `/healthz` ; l'image n'était jamais `healthy`.
  Révélé par la nouvelle bascule Docker.
- Démo `--no-k8s` : le runtime `argocd-k3s` restait configuré, injoignable, et bloquait
  la suppression des projets (elle passe par chaque runtime). Il n'est plus poussé, pas
  plus que le golden path `api-rest-go-k8s` ; `payment-api` passe sur `api-rest-go`.

## v0.6 — De vrais projets

### Ajouté
- **Démo avec de vrais projets** : `up.sh` crée et déploie trois services depuis les golden
  paths, via l'API de Symphony (`demo/seed.sh`) : `auth-service` (Python, Docker),
  `payment-api` (Go) et `notification-service` (Node) sur Kubernetes. Chacun a son repo,
  ses pipelines, son image et répond en HTTP ; `--no-seed` pour s'en passer.
- **GitLab connecté à Keycloak** en démo (OpenID Connect) : mêmes comptes pour Symphony et
  GitLab, créés et dotés de leurs droits par `up.sh`.
- Groupes GitLab en démo : `platform` (configuration, golden paths, repo de déploiement)
  et `apps` (les projets) ; `providers.scm[].config.namespace` choisit le groupe des
  repos créés.
- Le formulaire de création renseigne la fiche : équipe, tier, cycle de vie.

### Modifié
- **La fiche catalogue vit dans le repo du projet** (`catalog-info.yaml`, poussée avec le
  squelette, relue toutes les 15 s) au lieu de `symphony-config/services/` : son équipe la
  modifie par un commit. Un golden path peut en fournir la base (`skeleton/catalog-info.yaml`).
  `catalogue.repo`, `catalogue.path` et `CATALOGUE_BRANCH` ne sont plus lus ;
  `catalogue.file` change le nom de la fiche.
- GIF du README régénéré sur la nouvelle interface (Projets, détail d'un projet, Catalogue,
  création suivie en direct, Déploiements).
- **UI** : la page **Projets** devient l'accueil et réunit chaque projet, sa fiche (équipe,
  SLO, dépendances, liens, actions Day-2), ses pipelines et ses déploiements avec leur URL ;
  la page **Catalogue** présente les golden paths et crée les projets.
- `GET /api/v1/projects` renvoie la fiche de chaque projet (`service`).
- Un groupe GitLab introuvable à la création est une erreur, au lieu d'un repo créé à la
  racine du compte.
- `up.sh` nomme le port déjà pris et la variable qui le change.
- **Supprimer un projet supprime vraiment tout** : ses déploiements sur chaque runtime
  (conteneurs, volumes anonymes et images locales Docker ; Application Argo CD et ses
  ressources Kubernetes, manifests retirés du repo de déploiement), son historique de
  déploiements, et le repo GitLab pour de bon : GitLab 18 ne fait sinon que le
  programmer pour suppression (`-deletion_scheduled-`, gardé plusieurs jours).
- Les runtimes ont une méthode `Remove`.

## v0.5 — Day-2 asynchrone

### Ajouté
- **Tâches** : créer un projet, déployer, supprimer un projet et lancer une action Day-2
  sont des tâches suivies (`PENDING → RUNNING → SUCCESS | FAILED`), enregistrées en base
  étape par étape. L'API répond `202` avec la tâche ; une tâche interrompue par un
  redémarrage de Symphony reprend à son étape.
- La création d'un projet va jusqu'à la fin de sa **première pipeline** (image dans le
  registry), le déploiement jusqu'au **service prêt** (Argo CD Synced/Healthy ou
  conteneur démarré), une action pipeline jusqu'à la fin de sa pipeline.
- **Webhooks GitLab entrants** (`POST /webhooks/gitlab`) : Symphony abonne chaque repo qu'il
  crée aux événements de pipeline (section `webhooks` de la configuration, secret
  `WEBHOOK_SECRET`) ; un sondage toutes les 15 s sert de secours, et suffit seul sans
  webhooks (GitHub Actions).
- **Temps réel** : `GET /api/v1/tasks/events` (Server-Sent Events), `GET /api/v1/tasks`,
  `GET /api/v1/tasks/{id}`. L'UI montre chaque étape en direct, avec liens vers le repo, la
  pipeline en cours et le service ; nouvelle page **Activité** et compteur de tâches en cours.
- Métriques `symphony_tasks_finished_total`, `symphony_task_duration_seconds`,
  `symphony_webhooks_total`.

### Modifié
- `POST /projects`, `DELETE /projects/{name}`, `POST /deployments` et
  `POST /services/{name}/actions/{action}` répondent `202` avec une tâche ; `409` si une
  tâche est déjà en cours sur le même projet ou service.
- Nom de projet : un nom DNS (minuscules, chiffres, tirets, 63 caractères), utilisable tel
  quel pour Kubernetes.
- Échec d'une tâche journalisé dans l'audit (`<type>_failed`).

### Corrigé
- Golden paths de la démo : les pipelines Python échouaient (`pytest-cov` manquant) et
  Node aussi (`npm ci` sans `package-lock.json`) ; les quatre golden paths construisent et
  déploient leur image.
- GitLab : les écritures dans un même repo sont faites une à une (des commits simultanés,
  par exemple plusieurs suppressions de fiches catalogue, étaient refusés) ; un fichier
  ou un projet déjà supprimé n'est plus une erreur ; les redirections de l'API ne sont plus
  suivies vers une `external_url` injoignable.
- Déploiements : le formulaire se préremplit quand on arrive avec `?project=`.

## v0.4 — Kubernetes en GitOps

### Ajouté
- **Runtime `argocd`** : déployer sur Kubernetes sans que Symphony touche au cluster. Il
  commite les manifests du service (`Deployment`, `Service`, `Ingress`,
  `kustomization.yaml`) dans un repo de déploiement, en un commit, crée ou met à jour
  l'`Application` Argo CD (synchronisation automatique, `prune`, `selfHeal`) et lit son état.
  Arrêt (0 réplica) et relance sont aussi des commits ; les journaux passent par l'API
  d'Argo CD. Aucun identifiant de registry n'est écrit dans Git.
- **Kubernetes dans la démo** : k3s et Argo CD v3.1.8 installés par `./demo/up.sh`, repo
  `root/symphony-deployments`, compte Argo CD de Symphony aux droits limités, golden path
  « API REST Go (Kubernetes) » ; les services répondent sur
  `http://<service>.localhost:8081`. `--no-k8s` pour une démo plus légère.
- Page Déploiements : tous les runtimes à la fois, état Synced/Healthy d'Argo CD,
  relance, rafraîchissement automatique pendant un déploiement ; runtime choisi dans une
  liste (par défaut celui du projet).

### Modifié
- Un déploiement sans runtime précisé utilise celui du projet (donc de son golden path),
  plus le runtime par défaut.
- GitLab : pousser des fichiers qui existent déjà fait un seul commit (création et mise à
  jour mêlées) au lieu d'un commit par fichier.
- Lire un fichier absent d'un repo renvoie `ErrNotFound` (GitLab et GitHub).

### Retiré
- Le runtime `kubernetes`, qui n'était qu'un stub.

## v0.3 — Sécurité

### Ajouté
- **Connexion OIDC** en Backend-for-Frontend : Symphony fait le flux Authorization Code +
  PKCE et pose un cookie de session signé (`HttpOnly`, `SameSite=Lax`) ; le navigateur ne
  reçoit aucun jeton. Les scripts et la CI peuvent présenter un jeton `Bearer`.
- **Rôles** `viewer`, `developer`, `admin` déduits des groupes OIDC (section `auth.roles`
  de la configuration, rechargée à chaud), vérifiés route par route ; l'UI masque ce que le
  rôle ne permet pas et affiche l'utilisateur.
- **Protection CSRF** : une requête modifiante portée par le cookie doit venir de Symphony.
- **Keycloak dans la démo** : realm `symphony` avec alice (admin), bob (developer), carol
  (viewer) et dave (sans rôle).
- Audit des pipelines lancées à la main, des redémarrages et des synchronisations forcées.

### Modifié
- L'audit, les projets, pipelines et déploiements enregistrent l'utilisateur réel au lieu
  de `system`.
- CORS : plus de `*` ; seules les origines de `cors_origins` sont autorisées, sans cookies.
- En dev, le serveur Vite relaie `/api` et `/auth` vers Symphony (même origine).
- Sans section `auth`, l'authentification est désactivée avec un avertissement (tout le
  monde est admin) : pratique en local, à ne pas utiliser en production.

## v0.2 — Démo

### Ajouté
- **Démo en une commande** : `./demo/up.sh` démarre GitLab CE, un runner, PostgreSQL et
  Symphony, puis initialise GitLab (Auto DevOps désactivé, token API, repos
  `symphony-config` et `symphony-templates` avec 4 golden paths et 4 services d'exemple,
  runner enregistré). Rejouable ; ports réglables pour cohabiter avec une autre GitLab ;
  `./demo/down.sh` (`--purge`) arrête ou efface tout, y compris les services déployés.
- **Scénario de démo** (`demo/SCENARIO.md`) : catalogue, création depuis un golden path,
  pipeline, déploiement, pilotage par Git, métriques.
- **README** : pitch, GIF du parcours, schéma d'architecture, choix techniques, démarrage.
- `demo/capture/` : régénère le GIF du README avec Playwright.

### Corrigé
- Formulaire de création : le template CI du golden path ne s'affichait plus dans le
  résumé depuis le nouveau format des templates.

## v0.1 — Vitrine technique

### Ajouté
- **Binaire unique** : le front Vue est compilé dans `internal/web/dist` et embarqué avec
  `go:embed` ; un seul port sert l'UI et l'API, et l'image Docker (`scratch`) fait ~10 Mo.
- **`/metrics` Prometheus** : requêtes HTTP par route, appels à chaque provider (durée,
  erreurs), rechargements du catalogue, des templates et de la configuration.
- **CI GitHub Actions** : `gofmt`, `go vet`, `go test -race`, build du front, image
  publiée sur `ghcr.io`.

### Modifié
- Module Go renommé `github.com/clemuscle/symphony`.
- `catalogue.branch` est lu dans la configuration (`CATALOGUE_BRANCH` le surcharge) ;
  il était ignoré.
- En production, le front appelle l'API sur la même origine.
- Tout le code Go est formaté avec `gofmt`.

### Retiré
- `config/` (ancien catalogue local, inutilisé) et `script.deb.sh`.

## v0.0 — Point de départ

Création de projets depuis des golden paths, catalogue synchronisé depuis Git, golden paths
et configuration lus dans Git et rechargés à chaud, déploiement Docker avec bascule et
rollback. Voir [ROADMAP.md](ROADMAP.md#v00--point-de-départ-fait).
