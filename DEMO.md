# Démo Symphony — de zéro à un déploiement local

Ce guide fait tourner Symphony en local avec un vrai GitLab CE. Toute la
plomberie GitLab (groupe, projet d'infra, token, runner) est automatisée —
elle n'a aucune valeur pédagogique en soi, ce n'est que de la préparation.
La seule étape volontairement manuelle est le **wizard Symphony**
lui-même : c'est le produit qu'on démontre, le remplir toi-même est ce qui
a du sens ici.

## 1. Prérequis

- `docker` + `docker compose` (v2, plugin — vérifie avec `docker compose version`)
- `go` (pour lancer Symphony)
- `node` / `npm` (pour construire le frontend une fois)
- `jq` (pour le bootstrap automatique de GitLab)
- ~4 Go de RAM disponible confortablement (GitLab CE seul consomme ~2.5 Go) —
  ça peut tourner avec moins mais plus lentement
- Ports libres : `8929` (GitLab), `2224` (SSH GitLab), `5050` (registre),
  `5432` (PostgreSQL), `8090` (Symphony)

`make demo-up` (étape suivante) vérifie tout ça et te dit précisément ce
qui manque.

## 2. Lancer l'infra (entièrement automatisé)

```
make demo-up
```

Ce que ça fait, sans intervention de ta part :
1. Vérifie les prérequis, construit le frontend une fois si besoin.
2. Démarre PostgreSQL + GitLab CE + GitLab Runner (`docker-compose.demo.yml`),
   attend que GitLab réponde (~3-5 min au premier démarrage).
3. Désactive Auto DevOps (collisionnerait avec le `.gitlab-ci.yml` des
   golden paths).
4. Crée le groupe `symphony-demo` et le projet `symphony-demo/infra`
   (avec README — la synchro GitOps de Symphony lit son dernier commit dès
   le démarrage).
5. Génère un token SCM scopé au groupe et enregistre le runner GitLab
   (tag `docker`, socket Docker de l'hôte monté dans le conteneur).

Idempotent : rejouable sans tout redémarrer si une étape a besoin d'être
relancée (groupe/projet/runner déjà là → réutilisés ; le token est
régénéré à chaque run, GitLab ne permettant pas de relire sa valeur après
coup).

Pour suivre la progression en détail pendant l'attente de GitLab :

```
docker compose -f docker-compose.demo.yml --project-name symphony logs -f gitlab
```

À la fin, le script affiche le **token SCM** — garde-le sous les yeux pour
l'étape 4.

## 3. Démarrer Symphony

```
cp .env.demo.example .env
make demo-start
```

Ouvre [http://localhost:8090](http://localhost:8090). Comme aucun provider
n'est encore configuré, tu es automatiquement redirigé vers le wizard.

## 4. Wizard — configurer les providers (la seule étape manuelle)

Quatre étapes, une par catégorie de provider :

1. **SCM** — Type `gitlab`, URL `http://localhost:8929`, Token = celui
   affiché à la fin de `make demo-up`. Clique "Tester la connexion" avant
   de continuer.
2. **CI** — Type `gitlabci`, Dépôt de configuration = `symphony-demo/infra`,
   Dépôt des templates = laisse vide (pas utilisé dans ce MVP, les golden
   paths se chargent depuis `config/golden-paths/` en local). Token CI =
   laisse vide (réutilise le token SCM).
3. **Registry** — Type `gitlabregistry`, URL = laisse vide (déduite de
   l'URL SCM), Token = laisse vide.
4. **Deploy** — Type `docker`, Socket = `/var/run/docker.sock` (valeur par
   défaut). "Tester la connexion" doit répondre "Docker daemon
   accessible".

Termine par "Enregistrer & démarrer". Tu arrives sur la liste des projets
Symphony, providers actifs.

> Pourquoi cette étape reste manuelle alors que tout le reste est
> automatisé : c'est la configuration de Symphony lui-même, pas de la
> plomberie GitLab — la comprendre fait partie de la démo. Si tu veux
> uniquement une démo peuplée sans passer par le formulaire, l'étape 5
> ci-dessous reste à un seul `curl` de distance (voir le script
> `scripts/demo-seed.sh`, qui appelle `POST /api/v1/setup/save` — mais le
> faire une fois à la main donne une bien meilleure idée de ce que
> Symphony attend).

## 5. (Optionnel) Peupler plusieurs projets à différents stades

Pour voir d'un coup d'œil la diversité des statuts possibles plutôt que de
créer un projet à la main :

```
make demo-seed
```

Crée 4 projets via l'API publique de Symphony (aucune insertion directe en
base — mêmes statuts que ceux traversés en usage normal) :

| Projet | État |
|---|---|
| `demo-fresh` | Juste provisionné, aucun pipeline déclenché depuis Symphony |
| `demo-built` | Build automatique réussi (image visible dans **Images**), rien déployé |
| `demo-recette` | Recette de test déployée et active (`running`) |
| `demo-failed` | Déploiement tenté avant la fin du build — échec réel (`failed`), voir l'encart de l'étape 8 |

Prend plusieurs minutes (le runner de démo traite les jobs séquentiellement)
— le script affiche sa progression et est sûr à relancer.

## 6. Créer un projet toi-même

Dans l'UI Symphony, section **Projets** → **Nouveau projet** : choisis un
golden path (ex. *Go REST API*), donne-lui un nom, valide.

Symphony provisionne immédiatement (repo GitLab, projet CI, entrée
registre) et pousse le code de base + pipeline préconfiguré.

## 7. Observer le pipeline

Le push initial déclenche un pipeline GitLab avec 3 étapes qui se suivent :
**test** (`go test`/`pytest`/... selon le langage — voir `spec.test_tool` du
golden path) → **build** (construit l'image Docker **et la pousse** au
registre — c'est la même étape qui fait les deux, il n'y a pas de job
"push" séparé) → **register-service** (déclare le projet dans le catalogue
Symphony via GitOps). Suis-le directement dans GitLab
(`http://localhost:8929/symphony-demo/<projet>` → **CI/CD → Pipelines** —
connexion `root` / `SymphonyDemo2024!`, mot de passe fixé dans
`docker-compose.demo.yml` pour la démo uniquement, jamais en production).

> ⚠️ **Ce pipeline n'apparaît volontairement pas dans la fiche projet
> Symphony.** Symphony ne trace dans sa propre table de pipelines que ceux
> qu'il déclenche lui-même (bouton "Lancer pipeline", déploiement, recette)
> — jamais les pipelines déclenchés par un push git, y compris celui-ci.
> C'est pour ça que la fiche projet affiche "aucun pipeline lancé" même
> juste après un build réussi : c'est le comportement normal, pas un bug
> ni une fenêtre de course à attendre. La preuve côté Symphony qu'un build
> a réussi, c'est l'apparition de l'image dans la section **Images** de la
> fiche projet, pas un badge de pipeline.

Une fois `register-service` passé, le nouveau service apparaît dans le
catalogue Symphony (synchro GitOps depuis `symphony-demo/infra`, ~15s).

## 8. Déployer

Depuis la fiche du projet dans Symphony, clique **Déployer**. Symphony
délègue le déploiement au job `deploy` du pipeline CI (jamais exécuté
directement par Symphony — voir le principe d'architecture #3 du projet) ;
le statut passe `pending` → `running` une fois le pipeline terminé
(actualisation automatique côté UI, ou attends ~30s de réconciliation).

> ⚠️ **Le bouton "Lancer pipeline" de la fiche projet n'est pas un simple
> "rejouer les tests".** Le golden path route tout pipeline déclenché par
> Symphony (bouton "Lancer pipeline", ou l'action "Déployer" elle-même)
> vers le job `deploy` — il redéploie donc réellement l'environnement prod
> du projet à chaque clic, à condition qu'une image `:latest` existe déjà.
> `test`/`build` ne tournent que sur un vrai push git (voir l'encart
> ci-dessus), jamais sur un déclenchement Symphony. C'est pour ça que ce
> bouton est réservé au rôle `lead`, même niveau que "Déployer" — voir G1
> dans `documentation/backlog.md`. Si tu veux juste vérifier que les tests
> passent sur une branche, regarde le pipeline automatique de cette
> branche directement dans GitLab.

Si tu déploies **avant** que le build initial ait eu le temps de pousser
une image (`:latest` inexistante au registre), le job `deploy` échoue au
`docker pull` — déploiement affiché `failed`, comportement attendu, pas un
bug. C'est exactement ce que reproduit `demo-failed` (étape 5).

Vérifie que l'appli répond réellement :

```
curl localhost:<port>
```

(le port est celui affiché sur la fiche du projet, `8080` par défaut).

## 9. (Optionnel) Tester le RBAC multi-utilisateur

Pas nécessaire pour le reste de ce guide. Pour tester les rôles
`developer`/`lead`/`admin` de Symphony : dans GitLab, **Admin Area** (root)
→ **Users** → **New user** — crée par exemple `alice` (lead) et `bob`
(developer), ajoute-les au groupe `symphony-demo` (**Group → Members**)
avec les rôles Maintainer et Developer respectivement. `SYMPHONY_DEV_MODE=1`
(actif par défaut dans `.env.demo.example`) contourne entièrement les
contrôles de rôle — désactive-le pour observer un vrai `403`.

## 10. Nettoyage

```
make demo-down
```

Destructif — supprime les conteneurs **et** leurs volumes (GitLab, PostgreSQL).
À utiliser uniquement en fin de démo.
