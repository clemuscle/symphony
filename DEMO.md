# Démo Symphony — de zéro à un déploiement local

Ce guide fait tourner Symphony en local avec un vrai GitLab CE, en te
faisant faire toi-même chaque étape qui compte (connexion, tokens,
enregistrement du runner, configuration des providers). Rien n'est fait à
ta place côté GitLab — le but est de comprendre ce que Symphony automatise
ensuite pour toi une fois configuré.

Seule la partie mécanique (démarrer les conteneurs, attendre que GitLab
soit prêt) est scriptée.

## 1. Prérequis

- `docker` + `docker compose` (v2, plugin — vérifie avec `docker compose version`)
- `go` (pour lancer Symphony)
- `node` / `npm` (pour construire le frontend une fois)
- ~4 Go de RAM disponible confortablement (GitLab CE seul consomme ~2.5 Go) —
  ça peut tourner avec moins mais plus lentement
- Ports libres : `8929` (GitLab), `2224` (SSH GitLab), `5050` (registre),
  `5432` (PostgreSQL), `8090` (Symphony)

`make demo-up` (étape suivante) vérifie tout ça et te dit précisément ce
qui manque.

## 2. Lancer l'infra

```
make demo-up
```

Ce que ça fait : vérifie les prérequis ci-dessus, construit le frontend une
fois si besoin, démarre PostgreSQL + GitLab CE + GitLab Runner via
`docker-compose.demo.yml`, puis attend que GitLab réponde (~3-5 minutes au
premier démarrage — GitLab CE est lourd à initialiser).

Pour suivre la progression en détail pendant l'attente :

```
docker compose -f docker-compose.demo.yml --project-name symphony logs -f gitlab
```

Quand le script affiche `GitLab est prêt`, passe à l'étape suivante.

## 3. Se connecter à GitLab

Ouvre [http://localhost:8929](http://localhost:8929).

- Utilisateur : `root`
- Mot de passe : `SymphonyDemo2024!`

(mot de passe fixé dans `docker-compose.demo.yml` via
`gitlab_rails['initial_root_password']` — uniquement pour la démo, jamais
en production.)

## 4. Créer le groupe et le projet d'infra

Symphony a besoin d'un groupe pour y créer les projets golden path, et d'un
repo "infra" où il pousse la config de catalogue (services enregistrés,
lus par la synchro GitOps).

1. **New group** → nom `symphony-demo` (visibilité *Private* ou *Internal*,
   peu importe pour la démo).
2. Dans ce groupe, **New project** → *Create blank project* → nom `infra` →
   **coche "Initialize repository with a README"**.

   ⚠️ Cette case est importante : la synchro GitOps de Symphony lit le
   dernier commit du repo dès le démarrage — un repo totalement vide
   provoquerait une erreur (`no commits found`) tant qu'aucun commit
   n'existe.

Le chemin complet de ce projet — `symphony-demo/infra` — est la valeur
qu'on donnera à Symphony comme dépôt de configuration à l'étape 9.

## 5. (Optionnel) Créer des utilisateurs de démo

Pas nécessaire pour la suite de ce guide (golden path → déploiement), utile
seulement si tu veux tester le RBAC multi-utilisateur de Symphony.

**Admin Area** (menu du profil, en haut à droite) → **Users** → **New
user** — crée par exemple `alice` (lead) et `bob` (developer), ajoute-les
au groupe `symphony-demo` (**Group → Members**) avec les rôles Maintainer
et Developer respectivement.

## 6. Créer les tokens

Trois catégories de provider ont besoin d'un token GitLab — chacune avec
un scope minimal, pas un seul jeton fourre-tout. Le driver Deploy
(Docker local) n'a besoin d'aucun token : c'est un accès au socket Docker
de la machine, confirmé plus tard directement dans le wizard.

Tous les tokens de ce guide se créent au même endroit : dans le groupe
`symphony-demo` → **Settings** (barre latérale gauche) → **Access
Tokens** → **Add new token**.

### Token SCM (obligatoire)

- Nom : `symphony-scm`
- Rôle : *Owner* ou *Maintainer*
- Scope : `api`
- Expiration : une date dans le futur (ex. +1 an)

C'est le token qui permet à Symphony de créer des repos, pousser du
contenu, déclencher des pipelines. Copie-le immédiatement (GitLab ne le
réaffiche plus jamais) — tu le colleras dans le wizard à l'étape 9.

### Token CI (optionnel)

Par défaut, si tu laisses ce champ vide dans le wizard, Symphony réutilise
le token SCM. Pour illustrer un scope dédié plus étroit, tu peux créer un
second token du même type (`symphony-ci`, scope `api`) et le fournir
séparément.

### Token Registry (optionnel)

Même chose : vide dans le wizard = réutilise le token SCM. Pour un scope
minimal dédié au registre, crée un token `symphony-registry` avec les
scopes `read_registry` + `write_registry` uniquement (pas `api`) —
suffisant pour que Symphony liste/consulte les images du registre, sans
lui donner accès à la gestion des repos.

> Ne colle **aucun** de ces tokens dans un fichier `.env` à la main pour
> cette démo — ils se saisissent exclusivement dans le wizard Symphony
> (étape 9), qui les stocke correctement (jamais en clair dans
> `config/integrations.yaml`).

## 7. Enregistrer le runner GitLab

Le runner (conteneur `gitlab-runner`, déjà démarré par `make demo-up`)
n'est pas encore attaché à ton instance GitLab — il faut l'enregistrer une
fois.

1. **Admin Area** (root) → **CI/CD** → **Runners** → **New instance
   runner**.
2. Tags : `docker` (obligatoire — les golden paths de Symphony ciblent les
   runners tagués `docker` pour les étapes qui manipulent des containers).
3. Crée le runner — GitLab affiche une commande `gitlab-runner register`
   avec un token unique à cet endroit précis. Copie ce token.
4. Exécute la commande suivante (remplace `<TOKEN>` par le token copié) :

```
docker compose -f docker-compose.demo.yml --project-name symphony exec gitlab-runner \
  gitlab-runner register --non-interactive \
  --url http://gitlab:8929 \
  --token <TOKEN> \
  --executor docker \
  --docker-image docker:latest \
  --docker-volumes /var/run/docker.sock:/var/run/docker.sock \
  --docker-network-mode host \
  --tag-list docker \
  --description symphony-demo-runner
```

Le runner utilise le socket Docker de la machine hôte (monté dans le
conteneur runner) plutôt que du docker-in-docker — c'est pour ça que les
jobs `build`/`deploy` des golden paths utilisent
`DOCKER_HOST: unix:///var/run/docker.sock`.

Vérifie dans **Admin Area → CI/CD → Runners** que le runner apparaît en
ligne.

## 8. Démarrer Symphony

```
cp .env.demo.example .env
make demo-start
```

Ouvre [http://localhost:8090](http://localhost:8090). Comme aucun provider
n'est encore configuré, tu es automatiquement redirigé vers le wizard.

## 9. Wizard — configurer les providers

Quatre étapes, une par catégorie de provider :

1. **SCM** — Type `gitlab` (seul choix pour l'instant), URL
   `http://localhost:8929`, Token = le token SCM de l'étape 6. Clique
   "Tester la connexion" avant de continuer.
2. **CI** — Type `gitlabci`, Dépôt de configuration = `symphony-demo/infra`
   (le repo créé à l'étape 4), Dépôt des templates = laisse vide (pas
   utilisé dans ce MVP, les golden paths se chargent depuis
   `config/golden-paths/` en local). Token CI = optionnel (voir étape 6).
3. **Registry** — Type `gitlabregistry`, URL = laisse vide (déduite de
   l'URL SCM), Token = optionnel (voir étape 6).
4. **Deploy** — Type `docker`, Socket = `/var/run/docker.sock` (valeur par
   défaut). "Tester la connexion" doit répondre "Docker daemon
   accessible".

Termine par "Enregistrer & démarrer". Tu arrives sur le catalogue Symphony,
providers actifs.

## 10. Créer un projet

Dans l'UI Symphony, section **Projets** → **Nouveau projet** : choisis un
golden path (ex. *Go REST API*), donne-lui un nom, valide.

Symphony provisionne immédiatement (repo GitLab, projet CI, entrée
registre) et pousse le code de base + pipeline préconfiguré.

## 11. Observer le pipeline

Le push initial déclenche un pipeline GitLab : stages `test` → `build` →
`register-service`. Suis-le soit depuis la fiche projet dans Symphony
(steps + statut pipeline), soit directement dans GitLab
(`<groupe>/<projet>` → **CI/CD → Pipelines**).

Une fois `register-service` passé, le nouveau service apparaît dans le
catalogue Symphony (synchro GitOps depuis `symphony-demo/infra`, ~15s).

## 12. Déployer

Depuis la fiche du projet dans Symphony, clique **Déployer**. Symphony
délègue le déploiement au job `deploy` du pipeline CI (jamais exécuté
directement par Symphony — voir le principe d'architecture #3 du projet) ;
le statut passe `pending` → `running` une fois le pipeline terminé
(actualisation automatique côté UI, ou attends ~30s de réconciliation).

Vérifie que l'appli répond réellement :

```
curl localhost:<port>
```

(le port est celui affiché sur la fiche du projet, `8080` par défaut).

## 13. Nettoyage

```
make demo-down
```

Destructif — supprime les conteneurs **et** leurs volumes (GitLab, PostgreSQL).
À utiliser uniquement en fin de démo.
