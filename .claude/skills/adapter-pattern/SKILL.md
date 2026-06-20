---
name: adapter-pattern
description: Pattern générique pour intégrer un outil/service tiers derrière une interface stable (adapter, port, driver) — structure attendue, distinction entre opérations à faible risque (provisioning/lecture) et à fort risque (exécution/modification d'état vivant), checklist de conformité. Use PROACTIVELY dès qu'on ajoute une nouvelle intégration tierce derrière un contrat d'interface déjà défini dans le projet, ou qu'on révise un adaptateur existant. Contient un exemple complet appliqué à un projet Go (Symphony) en fin de document.
---

# Adapter Pattern

Un projet qui intègre plusieurs outils/services tiers (forges Git, CI,
registres d'artefacts, fournisseurs cloud, passerelles de paiement,
services de notification...) gagne à les isoler derrière un contrat
commun : une interface, un trait, un protocol, un port — peu importe le
nom dans le langage utilisé. Chaque intégration concrète devient une
implémentation de ce contrat, dans son propre module, jamais une branche
conditionnelle dans le code central.

## Quand utiliser ce skill

- Avant d'écrire un nouveau module d'intégration derrière un contrat
  d'interface déjà défini dans le projet
- Avant de modifier un adaptateur existant
- Avant de proposer d'étendre le contrat lui-même (ajouter une méthode à
  l'interface)
- Quand quelqu'un demande "comment ce projet parle à [outil tiers] ?"

## Comment démarrer sur un projet donné

1. Localise le fichier/module qui définit le contrat (interfaces) —
   son emplacement varie par projet, ne le suppose pas.
2. Localise une implémentation déjà écrite et fonctionnelle — c'est ta
   référence de style concrète pour ce projet précis, pas une
   convention générique importée d'ailleurs.
3. Repère si le projet documente une règle de risque (voir section
   suivante) — souvent dans le document d'architecture du projet
   (`CLAUDE.md` ou équivalent).

## La distinction qui prime sur le style de code : niveau de risque de l'opération

Avant le style, chaque méthode d'un adaptateur appartient à une de ces
deux catégories — et beaucoup de projets ont (ou devraient avoir) une
règle explicite sur comment les traiter différemment :

- **Provisioning / lecture** (créer une ressource administrative,
  lister, consulter un statut) : généralement un appel direct,
  synchrone, exécuté immédiatement. Ce sont des opérations de
  registration réversibles, à blast radius faible.
- **Exécution / modification d'un système vivant** (déployer, builder,
  appliquer une infra, débiter un paiement) : selon la politique du
  projet, ce type d'opération est souvent délégué à un système
  intermédiaire (pipeline, queue, orchestrateur) plutôt qu'exécuté
  directement par l'adaptateur — pour limiter le risque qu'une
  défaillance ou une montée de version de l'outil tiers affecte
  directement le cœur du projet.

Avant d'écrire une méthode d'adaptateur, identifie sa catégorie selon la
politique du projet. En cas de doute, demande plutôt que de trancher
silencieusement — c'est souvent la distinction la plus structurante
d'un produit qui orchestre des outils externes.

## Les éléments structurels attendus d'un adaptateur, quel que soit le langage

Détail et exemple concret dans `references/reference-implementation-walkthrough.md`.
Résumé :

1. **Configuration de connexion minimale**, avec une limite de temps
   d'attente toujours explicite — jamais d'appel bloquant sans borne.
2. **Un point d'entrée centralisé pour l'effet de bord principal**
   (l'appel réseau, la requête...) — pas de logique de connexion/auth
   dupliquée dans chaque méthode publique.
3. **Mapping explicite entre la forme externe et la forme interne**
   (DTO/struct dédiée), jamais de désérialisation directe dans un type
   interne partagé — ça isole le reste du projet d'un changement de
   format côté outil tiers.
4. **Erreurs contextualisées** : nom de l'adaptateur + opération +
   détail, jamais une erreur nue remontée telle quelle.
5. **Aucune erreur silencieusement avalée** — y compris les erreurs de
   désérialisation, souvent oubliées dans du code écrit vite.

## Checklist avant de livrer un nouvel adaptateur

Voir `assets/new-adapter-checklist.md` pour la liste complète. Version
courte :

- [ ] Implémente le contrat complet, signatures inchangées
- [ ] Limite de temps explicite sur les appels bloquants
- [ ] Erreurs contextualisées (adaptateur + opération + détail)
- [ ] Aucune erreur de désérialisation ignorée
- [ ] Catégorie de risque (provisioning/lecture vs exécution) identifiée
      et traitée selon la politique du projet
- [ ] Scope de credentials minimal documenté
- [ ] Pas de dépendance lourde si un appel direct simple suffit
- [ ] Pas de généralisation prématurée — voir skill
      `lazy-minimal-solution` : ce nouvel adaptateur répond-il à un
      besoin actuel confirmé, ou anticipe-t-il un cas hypothétique non
      demandé ?

## Pour aller plus loin

- `references/reference-implementation-walkthrough.md` — méthode de
  lecture d'une implémentation de référence dans un projet, avec un
  exemple ligne par ligne complet (Go/Symphony)
- `references/known-gaps-methodology.md` — comment repérer et
  documenter les lacunes d'une implémentation de référence sans les
  reproduire, avec exemple appliqué
- `assets/adapter-template.go` — squelette de départ en Go (à adapter
  au langage du projet si différent)
- `assets/new-adapter-checklist.md` — checklist complète imprimable

---

## Exemple appliqué — Symphony (IDP en Go)

Contrat : `internal/providers/interfaces.go`, 4 interfaces
(`SCMProvider`, `CIProvider`, `RegistryProvider`, `DeployProvider`).
Implémentation de référence : le driver GitLab
(`internal/providers/scm/gitlab/gitlab.go`).

| Interface | Rôle | Driver existant |
|---|---|---|
| `SCMProvider` | Gestion des dépôts de code | `scm/gitlab/gitlab.go` |
| `CIProvider` | Pipelines CI/CD | `ci/gitlabci/` |
| `RegistryProvider` | Registre d'artefacts/images | `artifacts/gitlabregistry/registry.go` |
| `DeployProvider` | Déploiement | `deploy/docker/docker.go` |

Règle de risque du projet (voir `CLAUDE.md`) : `SCMProvider.CreateRepo`
et la création de namespace/entrée registre = provisioning, appel
direct synchrone assumé. `CIProvider.TriggerPipeline` et
`DeployProvider.Deploy` = exécution applicative, toujours déléguée à un
pipeline externe (exception assumée et documentée :
`docker_local`, MVP local uniquement).

Signatures complètes : `references/interfaces.md`.