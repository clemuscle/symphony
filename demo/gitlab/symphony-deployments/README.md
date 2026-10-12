# symphony-deployments

Repo de déploiement GitOps : l'état voulu des services que Symphony déploie sur
Kubernetes (runtime `argocd`). Symphony y commite, Argo CD l'applique au cluster ; personne
n'exécute `kubectl apply`.

```
apps/<service>/
  kustomization.yaml   # tag de l'image, réplicas, redémarrages : ce qui change à chaque déploiement
  deployment.yaml      # le conteneur (image, port, variables d'environnement)
  service.yaml
  ingress.yaml         # http://<service>.localhost:<port>
```

Chaque dossier est la source d'une `Application` Argo CD du même nom, synchronisée
automatiquement (`prune`, `selfHeal`) : une modification faite à la main dans le cluster est
annulée, une modification commitée ici est appliquée. L'historique Git est donc l'historique
des déploiements : revenir en arrière, c'est faire un `git revert`.

Les identifiants du registry ne sont jamais écrits ici : le cluster a les siens.
