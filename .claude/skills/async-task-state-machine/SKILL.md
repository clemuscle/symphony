---
name: async-task-state-machine
description: Conventions pour modéliser et faire évoluer une state machine de tâches asynchrones persistée (ex. PENDING/RUNNING/SUCCESS/FAILED) — transitions valides, idempotence des notifications de retour (webhook/callback), reprise après redémarrage, audit trail. Use PROACTIVELY pour toute conception ou modification du suivi d'une action longue/asynchrone, ou pour toute nouvelle action déclenchée qui produit un résultat différé. Contient un exemple complet appliqué à un projet Go (Symphony) en fin de document.
---

# State Machine de tâches asynchrones

Un système qui orchestre des actions longues et asynchrones (déployer,
traiter un batch, déclencher un pipeline externe, appeler un service
tiers lent) a besoin d'un suivi fiable de leur progression, indépendant
de la mémoire du process qui les a déclenchées. C'est souvent la pièce
qui permet à un système de rester stateless tout en restant fiable.

## Le cycle de vie de référence

```
Requête → Créer la tâche (état initial, ex. PENDING)
        → Déclencher l'action externe
        → Mettre à jour l'état (en cours, ex. RUNNING)
        → [traitement effectif, potentiellement hors du process]
        → Notification de résultat (webhook, callback, poll)
        → Mettre à jour l'état final (ex. SUCCESS | FAILED)
        → L'UI/le consommateur reflète le résultat
```

Toute nouvelle fonctionnalité qui déclenche une action externe devrait
suivre ce cycle, sans en inventer un raccourci — par exemple ne jamais
passer directement d'aucune tâche à un état final sans étape
intermédiaire, même si l'action semble synchrone côté code appelant.

## États valides et transitions autorisées

Voir `references/state-diagram.md` pour le détail complet avec les cas
limites. Règle centrale, valable pour la quasi-totalité des state
machines de ce type :

- état initial → état intermédiaire : autorisé
- état intermédiaire → état final (succès ou échec) : autorisé
- état initial → état final échec directement : autorisé (rejet avant
  même le déclenchement, ex. validation échouée)
- état final → n'importe quel autre état : **interdit**. Un état final
  est final. Une notification tardive ou dupliquée qui tenterait de
  rouvrir une tâche déjà terminée doit être ignorée, jamais appliquée.

Cette règle existe spécifiquement pour gérer l'idempotence des
notifications de retour — voir section suivante.

## Idempotence des notifications de retour

Un système externe peut notifier plusieurs fois pour le même événement
(retry réseau, double-livraison), ou dans le désordre. Toute mise à jour
de statut doit :

1. Vérifier l'état actuel avant d'appliquer la transition (jamais une
   mise à jour aveugle qui écrase sans condition).
2. Rejeter/ignorer silencieusement (avec un log, pas une erreur visible
   côté utilisateur) toute transition qui partirait d'un état final.
3. Idéalement, identifier chaque notification par un identifiant unique
   côté système tiers, pour détecter une double-livraison exacte et pas
   seulement une transition incohérente.

## Reprise après redémarrage

Si le process redémarre pendant qu'une tâche est en cours, l'état en
base doit suffire à savoir quoi vérifier — sans dépendre de mémoire
perdue au redémarrage. Concrètement :

- Au démarrage, lister toutes les tâches encore dans un état
  intermédiaire (elles étaient en cours quand le process précédent
  s'est arrêté).
- Pour chacune, interroger le système externe concerné pour rattraper
  l'état réel plutôt que de la laisser indéfiniment dans cet état
  intermédiaire ("tâche fantôme").
- Si le système externe ne peut plus être interrogé (référence inconnue
  ou expirée), basculer la tâche vers un état final explicite avec une
  raison claire, plutôt que la laisser en suspens pour toujours.

## Audit trail

Toute transition d'état significative devrait produire une entrée
consultable (audit trail). Si une nouvelle catégorie de tâche est créée
sans que l'audit la couvre, c'est un oubli à corriger avant de livrer,
pas une option.

## Schéma minimal attendu

Voir `references/schema-guidance.md` pour le détail des colonnes
recommandées et leur justification — à confronter systématiquement au
schéma réel du projet avant de proposer une migration.

## Pour aller plus loin

- `references/state-diagram.md` — diagramme et cas limites des
  transitions
- `references/schema-guidance.md` — colonnes recommandées, index, choix
  de driver de base de données

---

## Exemple appliqué — Symphony (IDP en Go)

Symphony orchestre des actions longues (créer un repo, déclencher un
pipeline, déployer) en déléguant toujours l'exécution applicative à un
système externe (voir `CLAUDE.md`, règle provisioning/exécution). Le
cycle de référence du projet :

```
Requête UI → Créer Task (PENDING)
           → Push manifeste vers repo infra
           → Update Task (RUNNING)
           → [pipeline CI/CD externe s'exécute, hors de Symphony]
           → Webhook / poll de statut reçu
           → Update Task (SUCCESS | FAILED)
           → UI reflète le résultat
```

Cette state machine vit dans `internal/database/` (`deployments.go`,
`pipelines.go`). Au redémarrage, toute tâche `RUNNING` doit être
revérifiée via `GetPipelineStatus`/`Status` du driver concerné. Toute
transition produit une entrée consultable via `/api/v1/audit`
(`internal/database/audit.go`, exposé par `internal/api/server.go`).
