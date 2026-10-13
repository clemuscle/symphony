# Changelog

Les versions suivent [ROADMAP.md](ROADMAP.md). Chaque version publiée a son tag `vX.Y` et son
image `ghcr.io/clemuscle/symphony:X.Y`.

## Non publié

### Modifié
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
