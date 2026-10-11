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
- [ ] CI GitHub Actions : `go vet`, `go test -race`, build Go, build du front, image Docker
      publiée sur `ghcr.io/clemuscle/symphony`
- [ ] Binaire unique : le front compilé embarqué avec `go:embed`, servi par le serveur Go
      (même origine, plus besoin de `VITE_API_URL` en production)
- [ ] `/metrics` Prometheus : requêtes HTTP (latence, codes), appels aux providers (erreurs,
      latence), état des rechargements (catalogue, templates, configuration)
- [ ] `CHANGELOG.md`, tag `v0.1`

Fini quand : la CI passe sur `main`, `docker run ghcr.io/clemuscle/symphony` sert l'UI et
l'API sur un seul port, `/metrics` répond.

## v0.2 — Démo

Objectif : comprendre Symphony en 2 minutes, l'essayer en une commande.

- [ ] `docker compose` : GitLab CE, GitLab Runner, PostgreSQL, Symphony (≈ 4 Go de RAM,
      prérequis documentés)
- [ ] Script d'initialisation : token GitLab, repos `symphony-config` et
      `symphony-templates` avec les golden paths d'exemple, runner enregistré
- [ ] README en français : pitch, captures, GIF du parcours complet (golden path → repo →
      pipeline → déploiement → catalogue), schéma d'architecture, démarrage rapide
- [ ] Scénario de démo écrit (ce qu'on montre, dans quel ordre)

Fini quand : sur une machine vierge avec Docker, `docker compose up` puis le script donnent
une instance où l'on crée et déploie un service depuis l'UI.

## v0.3 — Sécurité

Objectif : plus d'API ouverte ; qui fait quoi est connu et tracé.

- [ ] Keycloak dans le `docker compose`, realm d'exemple (utilisateurs et groupes)
- [ ] Connexion OIDC (Authorization Code + PKCE), validation des jetons côté API
- [ ] RBAC par groupes : `viewer` (lecture), `developer` (créer, déployer, actions),
      `admin` (supprimer, synchroniser, audit)
- [ ] CORS restreint aux origines configurées
- [ ] Audit avec l'utilisateur réel au lieu de `system`

Fini quand : chaque route `/api/v1` exige un jeton, chaque rôle est testé, l'audit montre
qui a fait quoi.

## v0.4 — Kubernetes en GitOps

Objectif : déployer sur Kubernetes sans que Symphony applique quoi que ce soit lui-même.

- [ ] Cluster local k3d et Argo CD (script ou `docker compose`)
- [ ] Runtime `argocd` : Symphony écrit les manifests (Deployment, Service) dans un repo de
      déploiement séparé du code, crée l'`Application` Argo CD, lit son état de synchro et
      de santé
- [ ] Choix du runtime par golden path (`docker-local` ou `argocd`)
- [ ] Le runtime `kubernetes` (stub) est retiré ou remplacé

Fini quand : un service créé avec un golden path `argocd` tourne dans k3d, et son état
Argo CD (Synced/Healthy) est visible dans l'UI.

## v0.5 — Day-2 asynchrone

Objectif : les opérations longues sont des tâches suivies, pas des requêtes qui attendent.

- [ ] Table `tasks` et machine à états `PENDING → RUNNING → SUCCESS | FAILED`
- [ ] Webhooks entrants GitLab (événements de pipeline, secret vérifié) qui font avancer
      les tâches
- [ ] Création, déploiement, suppression et actions Day-2 deviennent des tâches
- [ ] Mise à jour de l'UI en temps réel (Server-Sent Events)

Fini quand : l'UI montre l'avancement d'une création ou d'un déploiement sans recharger la
page, et une tâche survit à un redémarrage de Symphony.

## v0.6 — Multi-plateforme et robustesse

- [ ] Golden path GitHub + GitHub Actions de bout en bout (workflow avec
      `workflow_dispatch` et entrées `SYMPHONY_*` déclarées)
- [ ] Bascule Docker : attendre le `HEALTHCHECK` de l'image au lieu de 3 s fixes
- [ ] Secrets dans Vault : la configuration référence un chemin Vault au lieu de
      `token_env`

## v1.0

- [ ] Documentation et démo à jour de toutes les fonctionnalités
- [ ] Tests d'intégration de bout en bout dans la CI (GitLab éphémère ou providers simulés)
- [ ] Release `v1.0`

## Hors périmètre

Haute disponibilité multi-réplicas (le catalogue et les suppressions en cours sont en
mémoire), Harbor, Jenkins, Terraform. Voir [les écarts avec KoreIDP](documentation/architecture.md#écarts-avec-la-proposition-koreidp).
