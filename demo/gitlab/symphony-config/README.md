# symphony-config

Configuration de Symphony, tenue par l'équipe plateforme (groupe `platform`).

```
symphony.yaml     # providers (scm, ci, registry, runtime, secrets, identity), catalogue,
                  # repo des golden paths, authentification
```

## symphony.yaml

Lu au démarrage de Symphony. Son `symphony.yaml` local n'est qu'un bootstrap qui
dit comment joindre ce repo :

```yaml
providers:
  secrets: # le jeton de GitLab vient lui-même de Vault
    - id: vault
      type: vault
      config:
        url: http://vault:8200
        role_id_env: VAULT_ROLE_ID
        secret_id_env: VAULT_SECRET_ID
config_source:
  scm:
    type: gitlab
    config:
      url: http://gitlab:8929
      token: secret://vault/symphony/gitlab#token
  repo: platform/symphony-config
  branch: main
  path: symphony.yaml
```

Relu toutes les 15 s : une version valide est appliquée à chaud (providers, repo
des templates, authentification), une version invalide est refusée et la
précédente reste en place (voir les logs de Symphony). Seul le bootstrap demande
un redémarrage.

Les secrets ne sont jamais écrits dans ce repo. Toute valeur de configuration d'un
provider peut être une référence `secret://<provider>/<chemin>#<clé>`, lue chez un
provider de la section `secrets` : `vault` (KV v2, AppRole ou jeton), ou `env`
(toujours présent : `secret://env/GITLAB_TOKEN`, comme l'ancien `token_env`). Les
secrets sont relus à chaque cycle : un jeton changé dans Vault est appliqué à chaud.

| Section | Types |
|---|---|
| `scm` | `gitlab`, `github` |
| `ci` | `gitlab_ci`, `github_actions` |
| `registry` | `gitlab_registry`, `ghcr` |
| `runtime` | `docker` (`health_timeout`), `argocd` |
| `secrets` | `vault`, `env` |
| `identity` | `keycloak` (connexion et annuaire), `oidc` (tout fournisseur OIDC) |

`auth.identity` désigne le provider identity de la connexion, `auth.roles` les groupes
qui donnent chaque rôle.

## Où sont les autres repos

- `platform/symphony-templates` : les golden paths (le catalogue de Symphony) ;
- `platform/symphony-deployments` : l'état voulu des services sur Kubernetes (Argo CD) ;
- `apps/*` : les projets créés par Symphony. Chacun porte sa fiche catalogue
  (`catalog-info.yaml`, à la racine) : son équipe la modifie par un commit.
