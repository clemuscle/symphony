# symphony-config

Source de vérité de Symphony.

```
symphony.yaml     # configuration : providers, catalogue, repo de templates
services/         # catalogue de services, un fichier par service (kind: Service)
```

## symphony.yaml

Lu au démarrage de Symphony. Son `symphony.yaml` local n'est qu'un bootstrap qui
dit comment joindre ce repo :

```yaml
config_source:
  scm:
    type: gitlab
    config:
      url: http://gitlab:8929
      token_env: GITLAB_TOKEN
  repo: root/symphony-config
  branch: main
  path: symphony.yaml
```

Relu toutes les 15 s : une version valide est appliquée à chaud (providers, repos
du catalogue et des templates), une version invalide est refusée et la précédente
reste en place (voir les logs de Symphony). Seul le bootstrap demande un redémarrage.

Les tokens ne sont jamais écrits dans ce repo : chaque provider nomme sa variable
d'environnement (`token_env`).

## services/

Relu toutes les 15 s. Symphony y écrit la fiche des projets qu'il crée et la
retire quand le projet est supprimé ; on peut aussi y ajouter des fiches à la main.

Les golden paths sont dans le repo `root/symphony-templates`.
