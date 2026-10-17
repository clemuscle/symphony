# symphony-stacks

Les stacks de la plateforme. Symphony les propose à côté de son catalogue officiel
(api-rest-go, api-rest-python, api-rest-node, worker-python) et relit ce repo toutes
les 15 s : pour ajouter une stack, pousser un dossier ; pour la retirer, le supprimer.
Une stack d'ici qui porte le nom d'une stack officielle la remplace.

```
stacks/<nom>/
  stack.yaml          # description, providers, paramètres du formulaire
  ci.gitlab-ci.yml    # CI du repo créé (ci_template, ou ci_templates par forge)
  skeleton/           # fichiers copiés à la racine du repo créé
```

Ici : les variantes de la démo, des copies des stacks officielles avec leurs
providers.

- `api-rest-go-k8s` : sur Kubernetes (Argo CD) dès staging, avec son chart Helm
  (paramètre `helm_chart` verrouillé par la plateforme) ;
- `api-rest-python-k8s` : sur Kubernetes dès staging, avec sa base PostgreSQL
  (CloudNativePG) dans chaque environnement (`spec.infra`) ;
- `api-rest-go-github`, `api-rest-python-github` : sur GitHub (Actions, GHCR),
  poussées seulement si la démo a un `GITHUB_TOKEN`.

## stack.yaml

Le format complet, commenté : `documentation/reference/stack.yaml` dans le repo de
Symphony. L'essentiel :

```yaml
apiVersion: symphony/v1
kind: Stack
metadata:
  name: api-rest-go-k8s      # = nom du dossier
  title: API REST Go (Kubernetes)
spec:
  language: go
  type: api
  providers:                 # ids de la configuration ; vides : ceux par défaut
    scm: gitlab-local
    runtime: argocd-k3s
  ci_template: ci.gitlab-ci.yml
  parameters:                # le formulaire de création
    - name: log_level
      title: Niveau de journalisation
      type: choice           # string, number, boolean, choice
      choices: [debug, info, warn, error]
      default: info
    - name: metrics_path
      type: string
      default: /metrics
      when: { metrics: true }  # n'existe que si metrics vaut true
  files:                     # fichiers du squelette créés sous condition
    - path: cmd/metrics.go
      when: { metrics: true }
  kubernetes:
    kustomize: deploy/kustomize  # ou helm: <dossier> ; sinon, manifests générés
  infra:                     # manifests d'opérateurs, une infra par environnement
                             # (avec un chart Helm, l'infra est dans le chart)
    kubernetes: deploy/infra
    env:
      DATABASE_URL: { secret: db-app, key: uri }
```

Dans le squelette et la CI, ces variables sont remplacées à la création :
`{{SERVICE_NAME}}`, `{{SERVICE_DESCRIPTION}}`, `{{LANGUAGE}}`, `{{TYPE}}`,
`{{PORT}}`, `{{YEAR}}` et `{{params.<nom>}}`. Un fichier en `.tmpl` est créé sans ce
suffixe (`go.mod.tmpl` → `go.mod`).

Une stack invalide (YAML, clé inconnue, fichier manquant, provider inconnu, CI
incompatible avec le SCM, paramètre mal décrit) est refusée seule ; la page
Administration de Symphony dit pourquoi. Vérifier avant de pousser :

```bash
symphony validate -config symphony.yaml -stacks stacks
```

## Fiche catalogue

Chaque projet créé reçoit une fiche `catalog-info.yaml` à la racine de son repo :
nom, équipe, tier et cycle de vie (saisis à la création), langage, liens vers le
repo et les pipelines, stack et manifests Kubernetes, et une action « Relancer la
pipeline ». Symphony la relit toutes les 15 s : l'équipe du projet la complète par
un commit (SLO, dépendances, liens, actions Day-2).

Une stack peut fournir sa propre base, `skeleton/catalog-info.yaml` : ses liens,
SLO et actions sont gardés, et Symphony y ajoute le reste.
