# Quickstart : Symphony sur votre forge

Symphony et sa base PostgreSQL en Docker Compose, sur une machine, branchés sur une
forge que vous avez déjà : **gitlab.com**, une **GitLab auto-hébergée** ou **GitHub**.
Les projets créés depuis l'interface ont leur repo et leur CI sur cette forge, et
tournent en conteneurs sur cette machine. Les stacks proposées sont celles du
[catalogue officiel](../stacks) : rien à pousser sur la forge pour commencer.

Pour tout voir sans forge (GitLab, Keycloak, Vault, Kubernetes et Argo CD compris) :
la [démo complète](../README.md#essayer). Pour une installation durable : le
[chart Helm](../deploy/helm/symphony) et la
[configuration de référence](../documentation/reference/symphony.yaml).

**Il faut** : Docker avec Compose, et un jeton de la forge.

## 1. Préparer la forge

- **Un jeton.** GitLab : jeton personnel (ou de groupe) au scope `api`. GitHub : jeton
  classique aux scopes `repo`, `workflow`, `delete_repo`, `read:packages`,
  `write:packages` et `delete:packages`.
- **Un groupe** (GitLab) ou une **organisation** (GitHub) pour les projets créés,
  facultatif : sans lui, ils vont dans le compte du jeton.

Sur gitlab.com, la CI des stacks construit les images avec un service
`docker:dind`. Sur une GitLab auto-hébergée dont les runners ne montent pas le socket
Docker, définir `DOCKER_DIND=true` dans les variables CI du groupe.

## 2. Configurer et lancer

```bash
cd quickstart
cp .env.example .env              # FORGE, FORGE_TOKEN, FORGE_GROUP… (voir les commentaires)
docker compose up -d
```

Ouvrir <http://localhost:8080>. Dans **Administration**, la forge, sa CI, le registry et
Docker sont vérifiés, et les stacks chargées (ou refusées, avec la raison) sont
listées. Depuis le **Catalogue**, choisir une stack, remplir son formulaire (niveau
de log, métriques…) et créer le projet : repo et première pipeline sur la forge, puis
**Déployer** depuis la page du projet. Le service répond sur
`http://localhost:<port>`.

`symphony.<forge>.yaml` est relu toutes les 15 s : une modification s'applique sans
redémarrer, une erreur est refusée et expliquée dans Administration.

## 3. Vos propres stacks

Créer un repo de stacks sur la forge (par exemple `mon-groupe/symphony-stacks`), y
copier une stack du [catalogue officiel](../stacks) dans `stacks/<nom>/` et l'adapter
(squelette, CI, paramètres : voir la [stack de référence](../documentation/reference/stack.yaml)),
puis renseigner `STACKS_REPO` dans `.env` et relancer `docker compose up -d`. Une
stack du repo qui porte le nom d'une stack officielle la remplace. Avant de pousser :

```bash
docker run --rm --env-file .env -v "$PWD:/w:ro" -w /w \
  ghcr.io/clemuscle/symphony:0.12 validate -config symphony.gitlab.yaml -stacks <dossier des stacks>
```

## Limites du quickstart

- **Pas d'authentification** : tout le monde est admin. Pour un usage à plusieurs,
  ajouter une section `auth` et un provider `identity` (OIDC : Keycloak, GitLab, Entra
  ID, Dex…), voir la configuration de référence.
- **Pas de webhooks** : la forge ne peut pas joindre cette machine, Symphony suit les
  pipelines par sondage (15 s).
- **Le jeton est dans `.env`** : pour la production, le mettre dans Vault ou OpenBao
  (`providers.secrets`) et ne donner à Symphony que son AppRole.
- Le runtime Docker sert pour un poste ou une petite équipe : il crée des conteneurs
  sur la machine (par un proxy de socket limité, voir
  [Sécurité](../documentation/architecture.md#sécurité)). Les charts Helm et bases
  Kustomize des stacks servent au runtime Kubernetes (Argo CD), pas ici.

Arrêter : `docker compose down` (`-v` efface aussi la base). Les repos créés restent
sur la forge : les supprimer depuis Symphony avant (bouton Supprimer d'un projet).
