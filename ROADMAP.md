# Roadmap de Symphony

Symphony est un projet de portfolio : une IDP que doit pouvoir évaluer un recruteur tech
ou un lead DevOps en quelques minutes (README, démo, architecture, qualité du code), puis
essayer s'il le souhaite. D'où l'ordre : **la vitrine d'abord, les fonctionnalités
ensuite**.

Chaque version est taguée (`vX.Y`), décrite dans `CHANGELOG.md` et publiée en release
GitHub avec son image Docker. Une version est finie quand tous ses critères sont remplis.
Le détail de l'architecture est dans [documentation/architecture.md](documentation/architecture.md).

## v0.0 — Point de départ (fait)

- [x] Création de projet depuis un golden path : repo, skeleton, CI, premier pipeline, fiche
      de catalogue, audit
- [x] Providers : GitLab et GitHub (SCM), GitLab CI et GitHub Actions, registry GitLab,
      Docker ; contrôle de compatibilité SCM/CI
- [x] Catalogue de services synchronisé depuis `symphony-config/services/`, actions Day-2
      (webhook ou pipeline, entrées validées)
- [x] Golden paths en dossiers autonomes dans `symphony-templates`, rechargés à chaud
- [x] Configuration lue dans Git (bootstrap local) et rechargée à chaud
- [x] Déploiement Docker : pull authentifié, port hôte stable, bascule avec rollback
- [x] Suppression de projet en arrière-plan (purge du registry GitLab)

## v0.1 — Vitrine technique

Objectif : un repo propre, une CI verte, un binaire unique observable.

- [x] Ménage : supprimer `config/` et `script.deb.sh` ; module Go
      `github.com/clemuscle/symphony` ; `gofmt` sur tout le code (commit dédié)
- [x] Lire `catalogue.branch` dans la configuration (`CATALOGUE_BRANCH` en surcharge)
- [x] CI GitHub Actions : `go vet`, `go test -race`, build Go, build du front, image Docker
      publiée sur `ghcr.io/clemuscle/symphony`
- [x] Binaire unique : le front compilé embarqué avec `go:embed`, servi par le serveur Go
      (même origine, plus besoin de `VITE_API_URL` en production)
- [x] `/metrics` Prometheus : requêtes HTTP (latence, codes), appels aux providers (erreurs,
      latence), état des rechargements (catalogue, templates, configuration)
- [x] `CHANGELOG.md`, tag `v0.1`

Fini quand : la CI passe sur `main`, `docker run ghcr.io/clemuscle/symphony` sert l'UI et
l'API sur un seul port, `/metrics` répond.

## v0.2 — Démo

Objectif : comprendre Symphony en 2 minutes, l'essayer en une commande.

- [x] `docker compose` : GitLab CE, GitLab Runner, PostgreSQL, Symphony (≈ 4 Go de RAM,
      prérequis documentés)
- [x] Script d'initialisation : token GitLab, repos `symphony-config` et
      `symphony-templates` avec les golden paths d'exemple, runner enregistré
- [x] README en français : pitch, captures, GIF du parcours complet (golden path → repo →
      pipeline → déploiement → catalogue), schéma d'architecture, démarrage rapide
- [x] Scénario de démo écrit (ce qu'on montre, dans quel ordre)

Fini quand : sur une machine avec Docker, `./demo/up.sh` donne une instance où l'on crée et
déploie un service depuis l'UI.

## v0.3 — Sécurité

Objectif : plus d'API ouverte ; qui fait quoi est connu et tracé.

- [x] Keycloak dans le `docker compose`, realm d'exemple (utilisateurs et groupes)
- [x] Connexion OIDC (Authorization Code + PKCE) en Backend-for-Frontend : le navigateur
      ne reçoit qu'un cookie de session signé ; jetons Bearer acceptés pour les scripts
- [x] RBAC par groupes : `viewer` (lecture), `developer` (créer, déployer, actions),
      `admin` (supprimer, synchroniser, audit)
- [x] CORS restreint aux origines configurées
- [x] Audit avec l'utilisateur réel au lieu de `system`

Fini quand : chaque route `/api/v1` exige une session ou un jeton, chaque rôle est testé,
l'audit montre qui a fait quoi.

## v0.4 — Kubernetes en GitOps

Objectif : déployer sur Kubernetes sans que Symphony applique quoi que ce soit lui-même.

- [x] Cluster local et Argo CD dans le `docker compose` de la démo : k3s (la distribution
      qu'utilise k3d, sans outil de plus à installer), Argo CD v3 installé par `up.sh`
      (`--no-k8s` pour s'en passer)
- [x] Runtime `argocd` : Symphony écrit les manifests (Deployment, Service, Ingress,
      Kustomize) dans un repo de déploiement séparé du code, crée l'`Application` Argo CD,
      lit son état de synchro et de santé ; arrêt et relance passent aussi par Git
- [x] Choix du runtime par golden path (`docker-local` ou `argocd-k3s`), modifiable au
      déploiement ; la page Déploiements montre tous les runtimes
- [x] Le runtime `kubernetes` (stub) est retiré

Fini quand : un service créé avec un golden path `argocd` tourne dans k3s, et son état
Argo CD (Synced/Healthy) est visible dans l'UI.

## v0.5 — Day-2 asynchrone

Objectif : les opérations longues sont des tâches suivies, pas des requêtes qui attendent.

- [x] Table `tasks` et machine à états `PENDING → RUNNING → SUCCESS | FAILED`, étapes
      enregistrées une à une et reprises au redémarrage
- [x] Webhooks entrants GitLab (événements de pipeline, secret vérifié) qui font avancer
      les tâches ; sondage du provider CI en secours
- [x] Création (jusqu'à la fin de la première pipeline), déploiement (jusqu'au service
      prêt), suppression et actions Day-2 deviennent des tâches
- [x] Mise à jour de l'UI en temps réel (Server-Sent Events) : avancement étape par étape,
      page Activité

Fini quand : l'UI montre l'avancement d'une création ou d'un déploiement sans recharger la
page, et une tâche survit à un redémarrage de Symphony.

## v0.6 — De vrais projets

Objectif (retours de la démo du 14 octobre 2026) : tout ce que montre la démo existe pour
de vrai, et chaque chose est à sa place.

- [x] Projets d'abord : la page Projets réunit chaque projet, sa fiche, ses pipelines et
      ses déploiements ; le Catalogue, ce sont les golden paths
- [x] Fiche catalogue dans le repo du projet (`catalog-info.yaml`), plus de fiche sans
      repo ni pipeline
- [x] Démo : trois projets créés depuis les golden paths et déployés (Docker et
      Kubernetes), avec repo, pipelines et URL
- [x] Démo : groupes GitLab `platform` (configuration) et `apps` (projets)
- [x] Démo : GitLab connecté au même Keycloak que Symphony
- [x] Supprimer un projet supprime tout : déploiements sur chaque runtime (conteneurs et
      images, Application Argo CD, ressources et manifests) et repo GitLab pour de bon
      (pas seulement programmé pour suppression)
- [x] GIF de la démo régénéré

Fini quand : après `./demo/up.sh`, trois projets répondent dans le navigateur, la fiche
d'un projet se modifie par un commit dans son repo, et bob se connecte à GitLab par Keycloak.

## v0.7 — Multi-plateforme et robustesse

Objectif : chaque service externe est un provider interchangeable, y compris les secrets
et l'identité ; GitHub marche de bout en bout comme GitLab.

- [ ] Golden path GitHub + GitHub Actions de bout en bout (workflow avec
      `workflow_dispatch` et entrées `SYMPHONY_*` déclarées) : squelette en un commit,
      statuts et id des runs, registry `ghcr`, package supprimé avec le projet
- [ ] Bascule Docker : attendre le `HEALTHCHECK` de l'image au lieu de 3 s fixes
      (`health_timeout`), et `HEALTHCHECK` dans l'image de Symphony
- [ ] Secrets en providers (`providers.secrets` : `vault`, `env`) : la configuration
      référence `secret://<provider>/<chemin>#<clé>` au lieu de `token_env`, rotation
      appliquée à chaud ; Vault (AppRole) dans la démo
- [ ] Identité en providers (`providers.identity` : `keycloak`, `oidc`) ; annuaire Keycloak :
      les groupes proposés comme équipe propriétaire à la création d'un projet

Fini quand : après `./demo/up.sh`, Symphony n'a plus aucun jeton dans son environnement
(seulement son AppRole), le formulaire de création propose les équipes de Keycloak, et
avec `GITHUB_TOKEN` un projet GitHub est créé, construit, déployé puis supprimé (repo et
package).

## v1.0

- [ ] Documentation et démo à jour de toutes les fonctionnalités
- [ ] Tests d'intégration de bout en bout dans la CI (GitLab éphémère ou providers simulés)
- [ ] Release `v1.0`

## Hors périmètre

Haute disponibilité multi-réplicas (le catalogue est en mémoire, un seul processus
exécute les tâches), Harbor, Jenkins, Terraform. Voir [les écarts avec KoreIDP](documentation/architecture.md#écarts-avec-la-proposition-koreidp).
