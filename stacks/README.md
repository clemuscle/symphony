# Stacks officielles

Le catalogue officiel de Symphony, embarqué dans le binaire : chaque dossier est
une stack (`stack.yaml`, une CI par forge, un squelette). Une instance les propose
quand sa configuration les expose, à côté des stacks de son propre repo :

```yaml
stacks:
  repo: platform/stacks       # facultatif : les stacks de l'entreprise
  official: ["*"]             # ou une liste : [api-rest-go, worker-python]
```

Une stack du repo qui porte le nom d'une stack officielle la remplace. Pour partir
d'une stack officielle et l'adapter, copier son dossier dans le repo de stacks.

| Stack | Paramètres | Kubernetes |
|---|---|---|
| `api-rest-go` | version de Go (verrouillée), niveau de log, métriques et leur chemin | chart Helm en option |
| `api-rest-python` | version de Python (verrouillée), niveau de log | base Kustomize en option |
| `api-rest-node` | version de Node.js (verrouillée), niveau de log | manifests générés |
| `worker-python` | version de Python (verrouillée), intervalle, niveau de log | manifests générés |

Elles ne désignent aucun provider : elles suivent ceux par défaut de la
configuration, GitLab (CI GitLab, registry GitLab) ou GitHub (Actions, GHCR).

Le format est décrit dans [la stack de référence](../documentation/reference/stack.yaml).
Les fichiers du squelette en `.tmpl` sont créés sans ce suffixe (`go.mod.tmpl` →
`go.mod`) : le module Go de Symphony ne les compile pas. Vérifier après une
modification :

```bash
go run ./cmd/symphony validate -stacks stacks
```
