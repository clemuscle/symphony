# symphony-templates

Golden paths de Symphony. Chaque dossier de
`templates/` est un golden path, rechargé par Symphony toutes les 15 s : pour en
ajouter un, pousser un dossier ; pour le retirer, le supprimer.

```
templates/<nom>/
  template.yaml       # description du golden path
  ci.gitlab-ci.yml    # template CI, écrit à l'emplacement du provider CI (.gitlab-ci.yml…)
  skeleton/           # fichiers copiés à la racine du repo créé
```

## template.yaml

```yaml
apiVersion: symphony/v1
kind: GoldenPath
metadata:
  name: api-rest-go          # identifiant, = nom du dossier (minuscules, chiffres, tirets)
  title: "API REST Go"
  description: "..."
  icon: go
spec:
  language: go
  type: api                  # api, worker…
  providers:                 # id des providers de symphony.yaml ; vide = provider par défaut
    scm: gitlab-local
    ci: gitlab-ci
    registry: gitlab-registry  # optionnel
    runtime: docker-local    # ou argocd-k3s : Kubernetes en GitOps (Argo CD)
  skeleton: skeleton         # dossier, relatif au template (défaut : skeleton)
  ci_template: ci.gitlab-ci.yml
```

Un template invalide (YAML, nom différent du dossier, clé inconnue, fichier manquant,
provider inconnu ou CI incompatible avec le SCM) est ignoré et signalé dans les logs
de Symphony ; les autres restent disponibles.

## Placeholders

Remplacés dans le skeleton et le template CI à la création du projet :
`{{SERVICE_NAME}}`, `{{SERVICE_DESCRIPTION}}`, `{{LANGUAGE}}`, `{{TYPE}}`, `{{PORT}}`, `{{YEAR}}`.

## Fiche catalogue

Chaque projet créé reçoit une fiche `catalog-info.yaml` à la racine de son repo :
nom, équipe, tier et cycle de vie (saisis à la création), langage, liens vers le
repo et les pipelines, et une action « Relancer la pipeline ». Symphony la relit
toutes les 15 s : l'équipe du projet la complète par un commit (SLO, dépendances,
liens, actions Day-2).

Un golden path peut fournir sa propre base, `skeleton/catalog-info.yaml` : ses
liens, SLO et actions sont gardés, et Symphony y ajoute le reste.

```yaml
apiVersion: symphony/v1
kind: Service
metadata:
  owner: team-payments       # le formulaire de création l'emporte
  tags: [rest-api]
spec:
  slo:
    availability: "99.9%"
  actions:
    - name: Scaler
      description: Change le nombre de réplicas
      inputs:
        - id: REPLICAS       # variable CI de la pipeline lancée par l'action
          type: integer
          min: 1
          max: 5
```
