# Roadmap de Symphony

Symphony est né comme un projet de portfolio : une IDP qu'un recruteur tech ou un lead
DevOps évalue en quelques minutes. Depuis la v0.10, **le produit passe avant la
vitrine** : la v1.0 doit pouvoir être adoptée par de vraies équipes. Le cap et les
décisions qui y mènent sont dans [documentation/vision.md](documentation/vision.md) :

> **Symphony, c'est un Cycloid open source qui s'administre comme un Backstage facile.**

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

- [x] Golden path GitHub + GitHub Actions de bout en bout (workflow avec
      `workflow_dispatch` et entrées `SYMPHONY_*` déclarées) : squelette en un commit,
      statuts et id des runs, registry `ghcr`, package supprimé avec le projet
- [x] Bascule Docker : attendre le `HEALTHCHECK` de l'image au lieu de 3 s fixes
      (`health_timeout`), et `HEALTHCHECK` dans l'image de Symphony
- [x] Secrets en providers (`providers.secrets` : `vault`, `env`) : la configuration
      référence `secret://<provider>/<chemin>#<clé>` au lieu de `token_env`, rotation
      appliquée à chaud ; Vault (AppRole) dans la démo
- [x] Identité en providers (`providers.identity` : `keycloak`, `oidc`) ; annuaire Keycloak :
      les groupes proposés comme équipe propriétaire à la création d'un projet

Fini quand : après `./demo/up.sh`, Symphony n'a plus aucun jeton dans son environnement
(seulement son AppRole), le formulaire de création propose les équipes de Keycloak, et
avec `GITHUB_TOKEN` un projet GitHub est créé, construit, déployé puis supprimé (repo et
package).

## v0.8 — Prêt pour la prod

Objectif (revue du 15 octobre 2026) : ce qu'un lead DevOps vérifie avant d'installer
Symphony chez lui tient debout.

- [x] Symphony s'installe sur Kubernetes : chart Helm (`deploy/helm/symphony` : non-root,
      système de fichiers en lecture seule, aucun accès à l'API Kubernetes), installé et
      vérifié par la CI sur un cluster kind
- [x] Configuration des providers validée au chargement : clé inconnue ou valeur requise
      absente refusée (comme les templates), plus de repli silencieux sur un jeton
- [x] Erreurs internes masquées : le client reçoit un message et un identifiant de requête,
      le détail va dans les logs
- [x] Compatibilité pré-1.0 retirée : `token_env` (→ `secret://env/…`), ancien format
      `auth.issuer`, avertissements `catalogue.repo`
- [x] Rôles par équipe : un projet appartient à une équipe de l'annuaire ; seuls ses
      membres (et les admins) le déploient, le suppriment ou lancent ses actions ; les
      groupes de rôles ne sont plus proposés comme équipes
- [x] Qualité : erreurs de décodage JSON vérifiées partout, `handlers/tasks.go` découpé
      par type de tâche, Go à jour (`go.mod`, CI, Dockerfile alignés)

Fini quand : `helm install` donne une instance qui marche, une faute de frappe dans
`symphony.yaml` est refusée avec un message clair, et bob ne peut pas supprimer un projet
d'une autre équipe.

## v0.9 — La vitrine

Objectif : le parcours complet d'une IDP, une évaluation possible en cinq minutes sans
rien installer, et plus aucun point faible visible à la relecture du code.

**Parcours**
- [x] Secrets des applis : une variable d'un service référence `secret://vault/…` ; le
      runtime `argocd` la fait lire dans le cluster (External Secrets), le runtime Docker
      la résout au déploiement ; jamais en clair dans Git ni dans l'API
- [x] Environnements : un projet a ses environnements (staging, production), chacun avec
      son runtime ; une version déployée en staging se **promeut** en production
- [x] GitHub à égalité avec GitLab : webhooks de workflow (plus de sondage seul), un
      deuxième golden path GitHub
- [x] Rotation du secret des webhooks : les repos des projets sont réabonnés avec le
      nouveau secret

**Qualité et sécurité**
- [x] Tests : handlers de l'API couverts ; tests d'intégration de bout en bout dans la CI
      (GitLab éphémère ou providers simulés)
- [x] Front : TypeScript, ESLint en CI, tests des stores et des vues clés, vues découpées
      en composants (`Projects.vue`)
- [x] Image non-root par défaut ; risque du socket Docker (runtime `docker`) documenté,
      avec un proxy de socket limité en démo
- [x] API décrite en OpenAPI, servie par Symphony ; identifiants de requête courts (sans
      le nom de la machine)

**Vitrine**
- [x] Mode visite sans GitLab (données de démo, lecture seule) pour essayer en une minute
- [x] Nouveau GIF, documentation et démo à jour de toutes les fonctionnalités

## v0.10 — Finitions

Objectif (vérification complète du 16 octobre 2026 : tests, démo k3s, parcours par l'API et
par l'interface) : corriger les petits défauts relevés avant d'attaquer la v1.0.

- [x] Interface : heures en français, l'activité dit l'environnement d'un déploiement et
      l'origine d'une promotion, cycle de vie distinct des environnements, pas d'erreur
      en console pendant la redirection vers la connexion
- [x] API : logs, relance et arrêt d'un déploiement retrouvent son runtime sans
      `?provider=` (404 trompeur auparavant)
- [x] Squelettes Go des golden paths formatés (vérifiés par la CI), plus d'avertissement
      « pas de .env » en conteneur, taille de l'image corrigée dans la doc

## v0.11 — L'admin y voit clair

Objectif : un admin plateforme sait, sans lire les logs, ce que Symphony a chargé, ce
qu'il a refusé et pourquoi ; il vérifie sa configuration et ses golden paths avant de
les pousser.

- [x] Licence Apache 2.0
- [x] Page Administration (admin) : configuration en place (source, révision, dernier
      refus et sa raison), providers et leur état (vérification à la demande, dernier
      appel), golden paths chargés et refusés avec la raison, fiches catalogue en
      erreur, journal d'audit
- [x] `symphony validate` : configuration et golden paths vérifiés hors ligne, en local
      ou dans la CI des repos de configuration (code de sortie, toutes les erreurs
      d'un coup)
- [x] Configuration de référence commentée (chaque clé, chaque type de provider),
      vérifiée par la CI
- [x] Quickstart léger : Symphony et PostgreSQL en Docker Compose, branchés sur une
      forge existante (gitlab.com, GitLab auto-hébergée ou GitHub)

Fini quand : une faute dans un golden path poussé dans Git se lit dans l'interface avec
sa raison, `symphony validate` la signale avant le push, et le quickstart donne une
instance qui crée un projet sur une forge existante.

## v0.12 — Stacks paramétrables

- [x] Paramètres d'une stack (`spec.parameters` : `name`, `title`, `type`,
      `choices`, `default`, `required`, `when`, `locked`), validés en JSON Schema,
      rendus en formulaire et injectés dans le squelette (`{{params.<nom>}}`, fichiers
      sous condition)
- [x] Manifests Kubernetes fournis par la stack (chart Helm ou base Kustomize),
      sinon générés comme aujourd'hui
- [x] Catalogue officiel de stacks (`stacks/` du repo public, embarqué dans le binaire),
      à côté de celles du client, exposé au choix de l'admin (`stacks.official`)
- [x] Vocabulaire tranché : « stack » (`stack.yaml`, `kind: Stack`, section `stacks`) ;
      l'ancien format est lu jusqu'à la v1.0 et signalé dans Administration

Fini quand : une stack officielle se remplit dans un formulaire borné (valeur verrouillée,
champ conditionnel) et donne un repo qui en tient compte ; une stack avec son chart Helm
ou sa base Kustomize tourne sur Kubernetes par Argo CD ; le quickstart propose les stacks
officielles sans rien pousser sur la forge.

## v0.13 — L'infra avec l'appli

- [x] Une stack porte son infra (`spec.infra`) : manifests d'opérateurs appliqués en
      GitOps avec le service, une infra par environnement ; Symphony la suit (santé sur
      la page du projet) sans détenir d'identifiants. OpenTofu, appliqué par la CI du
      repo, viendra avec les cibles cloud (AWS, Scaleway)
- [x] PostgreSQL par CloudNativePG de bout en bout : démo k3s (`auth-service` et sa
      base) et CI (kind)
- [x] Un projet supprimé emporte son infra (volumes compris) ; un objet retiré de Git
      n'est jamais élagué par erreur

Fini quand : un projet créé avec l'option PostgreSQL a sa base dans chaque
environnement, son appli s'y connecte sans que le mot de passe passe par Symphony, et sa
suppression ne laisse rien dans le cluster.

## v0.14 — Les services existants

- [x] Fiches compatibles Backstage (`backstage.io/v1alpha1`), nos ajouts en annotations
      ou champs étendus ; l'ancien format lu jusqu'à la v1.0
- [x] Import par scan d'un groupe GitLab ou d'une organisation GitHub, déclaré dans la
      configuration (`catalogue.discovery`) : sans rien écrire dans les repos, et
      retirer un projet découvert garde son repo
- [x] Interface : icônes des outils et pictogrammes à la place des émojis

Fini quand : une fiche Backstage existante est lue telle quelle, un repo poussé sans
Symphony dans un groupe scanné devient un projet qui se déploie, et le retirer laisse son
repo et ses images.

## v0.15 — L'équipe et la gouvernance

- [x] Rôle `lead` par équipe (viewer < developer < lead < admin)
- [x] Approbation par environnement (`approval: lead` ou `none`)
- [x] Notifications : Slack ou Teams par webhook (un canal par équipe), et dans Symphony
- [x] Matrice versions × environnements ; retour arrière en un clic

Fini quand : dans la démo, une promotion en production demandée par bob attend lina (lead
de son équipe), qui l'approuve depuis sa cloche ; l'équipe en est prévenue dans son
canal ; la page Versions montre où tourne chaque version, et un clic revient à la
précédente.

## v0.16 — Exploiter ses services

- [ ] Secrets en self-service (Vault ou OpenBao, Secrets Kubernetes / SOPS), écrits sous
      `apps/<projet>/` et jamais réaffichés
- [ ] Provider d'observabilité (Prometheus, Loki) : état, erreurs, latence et SLO par
      service et environnement
- [ ] Runtime Docker prêt pour la production : hôtes distants, TLS, reverse proxy

## v0.17 — Prêt à être adopté

- [ ] Site de documentation : installation, configuration de référence, écrire une
      stack, brancher Symphony sur ses outils
- [ ] Sauvegarde et restauration documentées et testées
- [ ] Nom tranché ; appel à bêta-testeurs

## v1.0 — Ouvert aux utilisateurs

Objectif : une équipe plateforme peut installer Symphony, le brancher sur ses outils, le
mettre à jour et s'y fier sans lire son code.

- [ ] Contrat stable (SemVer strict) : API `/api/v1`, configuration, fiches et stacks
      versionnées ; toute dépréciation annoncée une version à l'avance, avec son outil
      de migration ; les anciens formats des stacks (`template.yaml`, section
      `templates`) et des fiches (`symphony/v1`) retirés
- [ ] Montée de version testée d'une version à l'autre, retour arrière documenté
- [ ] Guide d'exploitation : installation (Compose et Helm), métriques et alertes,
      dépannage
- [ ] Sécurité revue : droits par route, dépendances surveillées (Dependabot,
      govulncheck), politique de signalement des failles
- [ ] Au moins une installation par un bêta-testeur, et ses retours traités

## Hors périmètre

Avant la v1.0 : haute disponibilité multi-réplicas (le catalogue est en mémoire, un seul
processus exécute les tâches), multi-organisation, plugins, interface en anglais,
TechDocs, scorecards, indicateurs DORA, Harbor, Jenkins. Symphony lui-même dans le k3s de la démo
(le chart Helm est installé et vérifié par la CI sur kind). Voir [les écarts avec KoreIDP](documentation/architecture.md#écarts-avec-la-proposition-koreidp).
