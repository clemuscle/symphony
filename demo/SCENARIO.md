# Scénario de démo

Le parcours complet d'un développeur, puis ce qui se passe côté équipe plateforme.
Compter 15 à 20 minutes, dont 3 à 4 d'attente de pipeline.

Organisation de la GitLab de démo :

- groupe **platform** (équipe plateforme, alice) : `symphony-config` (configuration de
  Symphony), `symphony-stacks` (les stacks propres à la plateforme, à côté du catalogue
  officiel de Symphony), `symphony-deployments` (l'état voulu
  des services sur Kubernetes, appliqué par Argo CD) ;
- groupe **apps** (développeurs : bob en Developer, carol en Reporter) : les projets créés
  par Symphony, chacun avec sa CI et sa fiche `catalog-info.yaml`.

Prérequis : `./demo/up.sh` a fini et affiche les URL de Symphony et de GitLab. Les exemples
ci-dessous utilisent les ports par défaut (Symphony sur 8080, GitLab sur 8929).

## 0. Se connecter, une fois pour tout (1 min)

Ouvrir http://localhost:8080 : Symphony renvoie vers Keycloak. Se connecter en **bob**
(mot de passe `bob`, rôle developer). L'en-tête affiche l'utilisateur et son rôle ; le
navigateur n'a reçu qu'un cookie de session, aucun jeton.

Ouvrir GitLab (http://localhost:8929) et cliquer **Keycloak** : bob est connecté sans
ressaisir de mot de passe. Keycloak est le seul annuaire : mêmes comptes, mêmes groupes
pour Symphony et GitLab.

## 1. Les projets (1 min)

La page **Projets** liste trois services créés par `up.sh` avec les stacks de la démo,
comme un développeur l'aurait fait : `auth-service` (Python, Docker), `payment-api`
(Go, Kubernetes, déployé par son chart Helm) et `notification-service` (Node, Kubernetes).
Ce sont de vrais projets :

- chaque carte montre son équipe, son tier et l'URL où il répond (un hello world) ;
- le panneau de détail réunit le repo, les pipelines, les déploiements (avec l'état
  Argo CD `Synced / Healthy`), la fiche (SLO, dépendances, liens) et les actions Day-2 ;
- **la fiche vient du repo du projet** : `catalog-info.yaml` à sa racine, relu toutes les
  15 s. Dans GitLab, `apps/auth-service` → modifier `catalog-info.yaml` (passer
  `availability` à `"99.9%"`, par exemple) : la carte change sans rien toucher dans Symphony.

Côté GitLab, bob voit le groupe **apps** (les projets, en Developer) mais pas **platform**
(la configuration de Symphony, tenue par alice).

## 2. Créer un service depuis le catalogue (4 min, suivi en direct)

**Catalogue** : les stacks approuvées par l'équipe plateforme, celles du catalogue
officiel (étiquette « officielle ») et celles de `platform/symphony-stacks`. Choisir
**API REST Go**, nommer le service `hello-api`, choisir l'équipe (le champ propose les
groupes de l'annuaire Keycloak : `team-payments`…) et le tier.

**Paramètres de la stack** : la version de Go est verrouillée (🔒, fixée par l'équipe
plateforme) ; choisir le niveau de log ; décocher « Exposer les métriques » fait
disparaître le champ « Chemin des métriques » (il n'existe que si les métriques sont
exposées) ; cocher **Chart Helm dans le repo** (le service sera déployé sur Kubernetes par
son propre chart). Une valeur hors des choix est refusée par l'API, qui valide le
formulaire avec le JSON Schema de la stack.

Créer : l'API répond tout de suite, la création est une **tâche**, dont les étapes
s'affichent en direct sous le formulaire.

1. **Repo et squelette** : le repo `apps/hello-api` est créé, le squelette (code,
   Dockerfile, tests, `deploy/chart/`), le `.gitlab-ci.yml` de la stack et la fiche
   `catalog-info.yaml` y sont poussés en un commit, ce qui lance la première pipeline.
   Le code tient compte des paramètres : niveau de log par défaut, `cmd/metrics.go`
   présent ou non ; la fiche note la stack et le chart (`spec.kubernetes.helm`), et la
   page du projet affiche les valeurs choisies.
2. **Projet et webhook** : le projet apparaît dans **Projets**, et le repo reçoit un
   webhook vers Symphony.
3. **Première pipeline** : `pipeline #N : pending → running → success`, poussé par le
   webhook de GitLab (le lien « Pipeline » ouvre la pipeline en cours). Elle teste puis
   construit l'image et la pousse dans le registry (3 à 4 min la première fois).

Le compteur **⚡ Activité** de l'en-tête montre les tâches en cours ; la page Activité les
liste toutes, avec leur auteur et leur durée.

**Une tâche survit à un redémarrage** : pendant la pipeline, redémarrer Symphony
(`docker compose restart symphony` dans `demo/`). La page se reconnecte seule et la
création reprend là où elle en était, sans recréer de repo.

## 3. Déployer en staging (1 min)

Tâche terminée : le lien **déployer hello-api →** ouvre la page **Déploiements** avec le
projet saisi ; l'image et ses tags sont préremplis depuis le registry, l'environnement
proposé est **staging** (production est grisée : « promotion seulement »). **Déployer** :
la tâche tire l'image, démarre le conteneur `hello-api-staging`, puis attend qu'il tourne ;
le service apparaît avec son URL (`/healthz` répond).

Redéployer ensuite : la bascule garde le même port et attend que le nouveau conteneur soit
`healthy` (le `HEALTHCHECK` du Dockerfile de la stack) avant de supprimer l'ancien ;
s'il ne le devient pas, l'ancien est relancé et la tâche montre la sortie du healthcheck.

## 3 bis. Promouvoir en production, sur Kubernetes en GitOps (2 min)

Dans **Projets**, `hello-api` : la section **Environnements** montre staging (Docker,
running) et production (Kubernetes, 🔒, non déployé). **⬆ Promouvoir depuis staging** :
la même image part en production, sur l'autre runtime. La tâche montre
`Argo CD : OutOfSync / Missing` puis `Synced / Healthy`, et le service répond sur
http://hello-api-production.localhost:8081/healthz. Un déploiement direct en production
est refusé (`403`, l'API le dit : « production est protégé »).

Symphony n'a rien appliqué lui-même :

- dans GitLab, `platform/symphony-deployments` a reçu un commit
  `deploy: hello-api-production …` avec `apps/hello-api-production/` : le chart Helm du
  repo du service (`chart/`), et le `values.yaml` de Symphony (image, réplicas,
  variables, Ingress) ;
- dans Argo CD (http://localhost:8088, identifiants affichés par `up.sh`), l'Application
  `hello-api-production` lit ce chart avec ces valeurs. Sans chart (case décochée), Symphony
  aurait généré les manifests.

**Stop** commite `replicaCount: 0` dans `values.yaml`, **Relancer** remet un réplica.
Modifier le cluster à la main ne tient pas : Argo CD le ramène à l'état de Git.

```bash
export KUBECONFIG=demo/k3s/kubeconfig.yaml
kubectl -n symphony-apps scale deploy hello-api-production --replicas=3   # revient à 1 en quelques secondes
```

**Secrets de l'appli.** `payment-api` (amorcé par la démo) lit son mot de passe de base dans
Vault : sa fiche déclare `DB_PASSWORD: secret://vault/apps/payment-api/db#password` pour
chaque environnement. En production (Kubernetes), le repo de déploiement ne contient
qu'une `ExternalSecret` (des références) ; External Secrets Operator crée le Secret dans
le cluster :

```bash
kubectl -n symphony-apps get externalsecret,secret payment-api-production-env
```

Une fiche qui viserait `secret://vault/symphony/gitlab#token` (le jeton de Symphony) ou les
secrets d'un autre projet ferait échouer le déploiement : un projet ne lit que
`apps/<projet>/`.

## 4. Côté plateforme : tout se pilote par Git (2 min)

Se connecter à GitLab en **alice** (bouton Keycloak) : elle tient le groupe **platform**.

**Ajouter une stack sans redémarrer.** Dans `platform/symphony-stacks`, dupliquer
`stacks/api-rest-go-k8s` en `stacks/api-rest-go-v2` (dans `stack.yaml`, changer `name` et
`title`). Dans Symphony, **Catalogue → ↻ Recharger depuis Git** : la nouvelle stack
apparaît. Une stack du repo qui porte le nom d'une stack officielle la remplace.

**Une stack cassée est refusée seule, pas fatale.** Dans ce `stack.yaml`, donner à un
paramètre un `default` hors de ses `choices` : la stack disparaît du Catalogue, les autres
restent, et la page **Administration** (en alice) dit laquelle et pourquoi.
`symphony validate -stacks stacks` l'aurait signalé avant le push.

**Changer la configuration à chaud.** Dans `platform/symphony-config/symphony.yaml`, retirer un
provider : il disparaît de `GET /api/v1/providers` en moins de 15 s. Une configuration
invalide (YAML cassé) est refusée, et la précédente reste en place.

**Aucun secret dans la configuration.** Le même `symphony.yaml` ne contient que des
références : `token: secret://vault/symphony/gitlab#token`. Vault (URL et jeton root
affichés par `up.sh`, `/ui`) les garde sous `secret/symphony/` ; Symphony n'a dans son
environnement que son AppRole (`docker compose exec symphony env` dans `demo/` : pas de
jeton GitLab). Rotation : dans Vault, créer une nouvelle version de
`secret/symphony/webhook` (secret de 16 caractères au moins) ; en moins de 15 s, les logs
disent « secret modifié dans son provider : configuration réappliquée ». Une référence
vers un secret absent fait refuser la configuration, la précédente reste en place.

**Tout est un provider.** `GET /api/v1/providers` liste aussi les providers `secrets`
(`env`, `vault`) et `identity` (`keycloak`) ; un autre coffre ou un autre fournisseur
OIDC se branche dans la même configuration.

## 4 bis. GitHub (5 min, démo lancée avec `GITHUB_TOKEN`)

Le catalogue montre **API REST Go (GitHub)**. Créer `hello-gh` : le repo est créé dans le
compte GitHub du jeton, le squelette poussé en un commit, et la tâche suit le workflow
GitHub Actions (tests, image poussée sur `ghcr.io/<compte>/hello-gh`). Déployer : l'image
est tirée de GHCR avec les identifiants du provider `ghcr`. L'action « Relancer la
pipeline » de la fiche lance le workflow par `workflow_dispatch`. Supprimer le projet
supprime aussi le repo GitHub et le package GHCR.

## 5. Les rôles et les équipes (2 min)

**Les droits suivent les équipes de l'annuaire.** bob (developer) est membre de
`team-payments` dans Keycloak : sur **Projets**, il déploie `payment-api` et lance ses
actions, mais `auth-service` (équipe `team-identity`) n'affiche que « réservé à l'équipe
team-identity et aux admins » ; l'API refuse de toute façon (`403`). À la création, le champ
équipe ne lui propose que les siennes. alice (admin) agit sur tous les projets.


**Déconnexion**, puis connexion en **carol** (viewer) : projets et catalogue visibles,
mais plus de création ni de formulaire de déploiement ; l'API refuse de
toute façon (`403`). **dave** est connecté mais sans rôle : Symphony le lui dit. **alice**
(admin) peut supprimer un projet et lire l'audit (`/api/v1/audit`), qui montre que `hello-api` a été créé
par `bob@demo.symphony`.

## 5 bis. Supprimer pour de bon (1 min)

Toujours en **alice** : **Projets**, supprimer `hello-api`. La tâche montre ses étapes :

1. **Déploiements** : l'Application Argo CD est supprimée avec ses ressources (pods,
   Service, Ingress), et `apps/hello-api-production/` quitte `platform/symphony-deployments` (commits
   `remove: hello-api-production`). Pour staging (Docker) : conteneur et images locales ; tous les environnements partent.
2. **Repo et images** : purge du registry (2 à 3 min sur GitLab), puis suppression
   **définitive** du repo : GitLab 18 ne ferait sinon que la programmer
   (`-deletion_scheduled-`, gardé plusieurs jours).
3. **Fiche catalogue** et **Projet** : la fiche est partie avec le repo ; plus rien en base.

À la fin, `kubectl -n symphony-apps get all` ne montre plus rien de `hello-api`, et
http://hello-api-production.localhost:8081 ne répond plus.

## 6. Observer (30 s)

http://localhost:8080/metrics : requêtes de l'API par route, appels à GitLab, Docker, Argo CD,
Vault et Keycloak par méthode (durée, erreurs), relectures des fiches, des stacks et de
la configuration.

## Nettoyer

`./demo/down.sh` arrête tout et supprime les services déployés par la démo ;
`./demo/down.sh --purge` efface aussi les données, pour repartir d'une démo neuve.
