# Diagramme d'états et cas limites

## Diagramme

```
                ┌─────────┐
        ┌──────►│ PENDING │
        │       └────┬────┘
        │            │ déclenchement effectif
        │            ▼
        │       ┌─────────┐
   création      │ RUNNING │
        │       └────┬────┘
        │            │
        │      ┌─────┴─────┐
        │      ▼           ▼
        │  ┌─────────┐ ┌────────┐
        │  │ SUCCESS │ │ FAILED │
        │  └─────────┘ └────────┘
        │   (final)      (final)
        │
        └─── PENDING → FAILED directement
             (rejet avant déclenchement, ex: validation d'input échouée)
```

## Cas limites à gérer explicitement

### 1. Webhook reçu pour une tâche déjà en état final

**Scénario** : le pipeline CI notifie deux fois le succès (retry réseau
côté CI après un timeout de réponse, alors que Symphony avait bien reçu
le premier webhook).

**Comportement attendu** : la deuxième notification est reconnue,
loggée, mais n'écrit rien de nouveau en base (idempotence). Ne jamais
retourner une erreur 500 au webhook pour ce cas — retourner 200 pour que
l'émetteur ne retente pas indéfiniment, mais ignorer côté traitement.

### 2. Webhook reçu dans le désordre

**Scénario** : un webhook "running" arrive après un webhook "success"
(latence réseau, files d'attente du côté de l'outil CI).

**Comportement attendu** : si l'état actuel est déjà `SUCCESS` ou
`FAILED`, ignorer toute transition entrante vers un état non-final.
Ne jamais laisser un webhook tardif "RUNNING" écraser un `SUCCESS` déjà
enregistré.

### 3. Tâche bloquée en RUNNING sans jamais recevoir de webhook final

**Scénario** : l'outil CI a un problème, le webhook n'est jamais émis ou
n'atteint jamais Symphony (problème réseau, mauvaise config d'URL de
callback).

**Comportement attendu** : ne pas compter uniquement sur le webhook.
Prévoir un mécanisme de vérification active (polling périodique via
`GetPipelineStatus`/`Status` du driver) pour les tâches `RUNNING` depuis
plus d'un délai raisonnable (ex: au-delà du temps habituel d'un
pipeline). Ce mécanisme peut être un job périodique simple plutôt qu'une
infrastructure de queue complexe — proportionné au MVP.

### 4. Redémarrage du process pendant qu'une tâche est RUNNING

Voir SKILL.md section "Reprise après redémarrage" — au démarrage,
lister les tâches `RUNNING` et revérifier leur statut réel via le driver
concerné avant de les laisser dans cet état.

### 5. Action rejetée avant même la création de la tâche

**Scénario** : une requête de déploiement arrive avec un input invalide
(ex: `replica_count` hors des bornes `min`/`max` définies dans
`config/services/payment-api.yaml`).

**Comportement attendu** : rejeter en amont avec une erreur 400 claire,
sans même créer de ligne `PENDING` en base. Une tâche ne doit exister en
base que si elle a une chance réelle d'aboutir à une exécution — pas
créer puis immédiatement marquer `FAILED` pour une erreur de validation
pure, qui pollue l'audit trail avec du bruit sans valeur.
