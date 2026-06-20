---
name: api-contract-discipline
description: Conventions pour maintenir un contrat API cohérent entre un backend et ses consommateurs (frontend, autres services) — codes HTTP, format d'erreur uniforme, nommage des champs, synchronisation des routes. Particulièrement important dans un monorepo sans génération automatique de client (pas d'OpenAPI/gRPC générant le code client). Use PROACTIVELY pour toute nouvelle route, tout changement de payload, ou toute synchronisation entre le code serveur et son consommateur. Contient un exemple complet appliqué à un projet Go+Vue (Symphony) en fin de document.
---

# Discipline du contrat API

Dans un monorepo sans génération de client automatique, le contrat entre
un backend et ses consommateurs (frontend, autres services) est
maintenu manuellement des deux côtés — ce qui crée un risque de dérive
silencieuse. Ce skill fixe les règles pour limiter ce risque.

## Avant toute intervention

1. **Cartographie les routes existantes** depuis le routeur réel — ne
   suppose jamais une liste de routes mémorisée, vérifie-la dans le
   code à chaque fois qu'elle pourrait avoir changé.
2. **Repère le point central d'appel côté consommateur** (fichier
   d'API centralisé, client généré, hooks dédiés). S'il n'existe pas,
   c'est en soi un signal : des appels HTTP dispersés rendent le
   contrat invérifiable.
3. **Repère le format d'erreur déjà en usage**, s'il existe — ne pas en
   inventer un nouveau en parallèle.

## Codes de statut HTTP — convention à respecter

| Situation | Code |
|---|---|
| Création réussie | 201 |
| Lecture réussie | 200 |
| Suppression réussie | 200 ou 204 |
| Erreur de validation d'input utilisateur | 400 |
| Authentification manquante/invalide | 401 |
| Action non autorisée | 403 |
| Ressource introuvable | 404 |
| Conflit (état incompatible avec l'action demandée) | 409 |
| Erreur interne imprévue | 500 |
| Échec d'un appel à un système tiers en amont | 502 |

Une erreur de validation d'input n'est **jamais** un 500 — c'est
toujours une 400. Réserver 500 aux échecs réellement inattendus côté
serveur.

## Format d'erreur uniforme

Toutes les routes devraient renvoyer la même forme JSON en cas
d'erreur. Recommandation si aucune n'existe encore :

```json
{
  "error": "message lisible pour le développeur/l'utilisateur",
  "code": "validation_error"
}
```

Le champ `code` (machine-readable, snake_case stable) permet au client
de réagir différemment selon le type d'erreur sans parser le texte du
message. Si un format différent existe déjà dans le code, le
documenter et s'y conformer plutôt que d'en introduire un deuxième en
parallèle.

## Nommage des champs

Choisir une convention de casse unique (camelCase ou snake_case côté
JSON) et l'appliquer systématiquement — ne jamais mélanger les deux
selon l'endpoint. Vérifier la convention déjà en usage dans les types
existants avant d'en introduire une nouvelle.

## Alignement avec le consommateur

Toute nouvelle route côté serveur doit avoir son pendant explicite dans
le point central d'appel côté client — jamais un appel ad hoc qui le
contourne. Voir `references/sync-checklist.md` pour la procédure de
vérification à chaque changement de contrat.

## CORS / origines autorisées

Si le projet expose une API consommée par un frontend séparé, la
configuration CORS doit rester restrictive à des origines explicites.
Si élargie temporairement pendant le développement local, s'assurer
qu'un ticket/TODO existe pour la resserrer avant un déploiement
au-delà du poste de dev.

## Handlers minces

Les handlers HTTP doivent rester de purs traducteurs requête/réponse,
délégant la logique métier à une couche service/domaine. Toute logique
de décision complexe, orchestration multi-étapes, ou polling qui
s'installerait directement dans un handler est un signal à remonter à
l'agent d'architecture du projet plutôt qu'à laisser grossir.

## Pour aller plus loin

- `references/sync-checklist.md` — checklist de synchronisation
  backend/frontend à chaque changement de route
- `references/error-format-examples.md` — exemples de réponses
  d'erreur pour des cas fréquents (validation, conflit, ressource
  introuvable), illustrés sur Symphony

---

## Exemple appliqué — Symphony (IDP en Go, chi router + Vue)

Monorepo Go+Vue sans génération de client. Routes (`internal/api/server.go`,
à revérifier avant toute intervention) :

```
GET    /healthz
GET    /api/v1/services
GET    /api/v1/services/{name}
POST   /api/v1/services/{name}/actions/{action}
GET    /api/v1/golden-paths
POST   /api/v1/templates/reload
POST   /api/v1/projects
GET    /api/v1/projects
GET    /api/v1/repos
POST   /api/v1/pipelines/trigger
GET    /api/v1/pipelines/status
GET    /api/v1/pipelines/{project}
GET    /api/v1/deployments
POST   /api/v1/deployments
DELETE /api/v1/deployments/{id}
GET    /api/v1/audit
```

Point central côté client : `frontend/src/api.js`. Nommage JSON :
vérifier la convention en usage dans `internal/database/` et
`internal/catalog/types.go` avant d'en introduire une nouvelle. Handlers
(`handlers.go`, `projects.go`) doivent rester minces — toute orchestration
complexe détectée dedans est un signal pour `architecture-guardian`.

`corsMiddleware` (déjà présent dans `server.go`) doit rester restrictif.
