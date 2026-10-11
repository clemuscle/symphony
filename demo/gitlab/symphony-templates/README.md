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
    runtime: docker-local
  skeleton: skeleton         # dossier, relatif au template (défaut : skeleton)
  ci_template: ci.gitlab-ci.yml
```

Un template invalide (YAML, nom différent du dossier, clé inconnue, fichier manquant,
provider inconnu ou CI incompatible avec le SCM) est ignoré et signalé dans les logs
de Symphony ; les autres restent disponibles.

## Placeholders

Remplacés dans le skeleton et le template CI à la création du projet :
`{{SERVICE_NAME}}`, `{{SERVICE_DESCRIPTION}}`, `{{LANGUAGE}}`, `{{TYPE}}`, `{{PORT}}`, `{{YEAR}}`.
