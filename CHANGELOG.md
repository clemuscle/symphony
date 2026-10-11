# Changelog

Les versions suivent [ROADMAP.md](ROADMAP.md). Chaque version publiée a son tag `vX.Y` et son
image `ghcr.io/clemuscle/symphony:X.Y`.

## v0.2 — Démo

### Ajouté
- **Démo en une commande** : `./demo/up.sh` démarre GitLab CE, un runner, PostgreSQL et
  Symphony, puis initialise GitLab (Auto DevOps désactivé, token API, repos
  `symphony-config` et `symphony-templates` avec 4 golden paths et 4 services d'exemple,
  runner enregistré). Rejouable ; ports réglables pour cohabiter avec une autre GitLab ;
  `./demo/down.sh` (`--purge`) arrête ou efface tout, y compris les services déployés.
- **Scénario de démo** (`demo/SCENARIO.md`) : catalogue, création depuis un golden path,
  pipeline, déploiement, pilotage par Git, métriques.
- **README** : pitch, GIF du parcours, schéma d'architecture, choix techniques, démarrage.
- `demo/capture/` : régénère le GIF du README avec Playwright.

### Corrigé
- Formulaire de création : le template CI du golden path ne s'affichait plus dans le
  résumé depuis le nouveau format des templates.

## v0.1 — Vitrine technique

### Ajouté
- **Binaire unique** : le front Vue est compilé dans `internal/web/dist` et embarqué avec
  `go:embed` ; un seul port sert l'UI et l'API, et l'image Docker (`scratch`) fait ~10 Mo.
- **`/metrics` Prometheus** : requêtes HTTP par route, appels à chaque provider (durée,
  erreurs), rechargements du catalogue, des templates et de la configuration.
- **CI GitHub Actions** : `gofmt`, `go vet`, `go test -race`, build du front, image
  publiée sur `ghcr.io`.

### Modifié
- Module Go renommé `github.com/clemuscle/symphony`.
- `catalogue.branch` est lu dans la configuration (`CATALOGUE_BRANCH` le surcharge) ;
  il était ignoré.
- En production, le front appelle l'API sur la même origine.
- Tout le code Go est formaté avec `gofmt`.

### Retiré
- `config/` (ancien catalogue local, inutilisé) et `script.deb.sh`.

## v0.0 — Point de départ

Création de projets depuis des golden paths, catalogue synchronisé depuis Git, golden paths
et configuration lus dans Git et rechargés à chaud, déploiement Docker avec bascule et
rollback. Voir [ROADMAP.md](ROADMAP.md#v00--point-de-départ-fait).
