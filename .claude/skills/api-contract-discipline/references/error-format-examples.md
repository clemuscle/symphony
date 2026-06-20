# Exemples de réponses d'erreur — cas fréquents Symphony

Ces exemples illustrent le format uniforme proposé dans `SKILL.md`. À
adapter si un format différent est déjà établi dans le code réel.

## Validation d'un input d'action (ex: "Scale replicas")

`config/services/payment-api.yaml` définit `replica_count` avec
`min: 1`, `max: 20`. Si une requête envoie `replica_count: 50` :

```
HTTP 400
{
  "error": "replica_count doit être compris entre 1 et 20 (reçu: 50)",
  "code": "validation_error"
}
```

## Conflit — déploiement déjà actif

```
HTTP 409
{
  "error": "un déploiement est déjà actif pour ce projet, arrêtez-le avant d'en lancer un nouveau",
  "code": "deployment_conflict"
}
```

## Ressource introuvable

```
HTTP 404
{
  "error": "service 'payment-api-typo' introuvable dans le catalogue",
  "code": "not_found"
}
```

## Échec d'un appel à un provider externe (ex: GitLab indisponible)

Ne jamais transmettre l'erreur brute du driver telle quelle au frontend
(elle peut contenir des détails internes non pertinents pour
l'utilisateur, voire sensibles). La traduire :

```
HTTP 502
{
  "error": "le service GitLab est actuellement indisponible, réessayez dans quelques instants",
  "code": "upstream_unavailable"
}
```

Le détail technique complet de l'erreur driver doit néanmoins être loggé
côté serveur (pas perdu) pour le debug, juste pas exposé tel quel au
client.
