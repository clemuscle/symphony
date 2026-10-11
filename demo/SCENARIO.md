# Scénario de démo

Le parcours complet d'un développeur, puis ce qui se passe côté équipe plateforme.
Compter 10 minutes, dont 3 à 4 d'attente de pipeline.

Prérequis : `./demo/up.sh` a fini et affiche les URL de Symphony et de GitLab. Les exemples
ci-dessous utilisent les ports par défaut (Symphony sur 8080, GitLab sur 8929).

## 1. Le catalogue (30 s)

Ouvrir **Catalogue** sur http://localhost:8080 : quatre services d'exemple, avec équipe,
niveau de criticité, SLO et liens. Ces fiches ne sont pas en base : elles viennent du repo
`root/symphony-config` (`services/`), relu toutes les 15 s.

## 2. Créer un service depuis un golden path (1 min)

**Projets → Nouveau projet**, choisir **API REST Go**, nommer le service `hello-api`,
créer. En un clic, Symphony :

- crée le repo `root/hello-api` dans GitLab ;
- y pousse le squelette (code, Dockerfile, tests) et le `.gitlab-ci.yml` du golden path, en
  un seul commit, ce qui lance la première pipeline ;
- ajoute la fiche `hello-api` au catalogue (visible tout de suite dans **Catalogue**).

Dans GitLab (http://localhost:8929/root/hello-api/-/pipelines) : la pipeline teste puis
construit l'image et la pousse dans le registry du projet (3 à 4 min la première fois).

## 3. Déployer (1 min)

Quand la pipeline est verte, page **Déploiements**, formulaire « Nouveau déploiement » :
saisir le projet `hello-api`, l'image et ses tags sont préremplis depuis le registry.
**Déployer** : Symphony tire l'image et démarre le conteneur, qui apparaît dans la liste
avec son URL (`/healthz` répond).

Redéployer ensuite le même service : la bascule garde le même port et, si le nouveau
conteneur ne démarre pas, l'ancien est relancé.

## 4. Côté plateforme : tout se pilote par Git (2 min)

**Ajouter un golden path sans redémarrer.** Dans GitLab, `root/symphony-templates`,
dupliquer `templates/api-rest-go` en `templates/api-rest-go-v2` (dans `template.yaml`,
changer `name` et `title`). Dans Symphony, **Projets → Nouveau projet →
↻ Recharger depuis Git** : le nouveau golden path apparaît.

**Un template cassé est ignoré, pas fatal.** Dans ce `template.yaml`, mettre un `name`
différent du dossier : le golden path disparaît de la liste, les autres restent, et les logs
(`docker compose logs symphony` dans `demo/`) disent pourquoi.

**Changer la configuration à chaud.** Dans `root/symphony-config/symphony.yaml`, retirer un
provider : il disparaît de `GET /api/v1/providers` en moins de 15 s. Une configuration
invalide (YAML cassé) est refusée, et la précédente reste en place.

## 5. Observer (30 s)

http://localhost:8080/metrics : requêtes de l'API par route, appels à GitLab et à Docker par
méthode (durée, erreurs), rechargements du catalogue, des templates et de la configuration.

## Nettoyer

`./demo/down.sh` arrête tout et supprime les services déployés par la démo ;
`./demo/down.sh --purge` efface aussi les données, pour repartir d'une démo neuve.
