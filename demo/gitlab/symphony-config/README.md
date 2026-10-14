# symphony-config

Configuration de Symphony, tenue par l'équipe plateforme (groupe `platform`).

```
symphony.yaml     # providers, catalogue, repo des golden paths, authentification
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
  repo: platform/symphony-config
  branch: main
  path: symphony.yaml
```

Relu toutes les 15 s : une version valide est appliquée à chaud (providers, repo
des templates, authentification), une version invalide est refusée et la
précédente reste en place (voir les logs de Symphony). Seul le bootstrap demande
un redémarrage.

Les tokens ne sont jamais écrits dans ce repo : chaque provider nomme sa variable
d'environnement (`token_env`).

## Où sont les autres repos

- `platform/symphony-templates` : les golden paths (le catalogue de Symphony) ;
- `platform/symphony-deployments` : l'état voulu des services sur Kubernetes (Argo CD) ;
- `apps/*` : les projets créés par Symphony. Chacun porte sa fiche catalogue
  (`catalog-info.yaml`, à la racine) : son équipe la modifie par un commit.
