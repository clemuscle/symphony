---
name: declarative-scaffolding
description: Comment concevoir un mécanisme de génération de projet/ressource (scaffolding, golden path, boilerplate) piloté par une configuration déclarative rechargeable, plutôt que codé en dur dans le langage du projet. Use PROACTIVELY dès qu'on conçoit ou modifie un flux "créer un nouveau X à partir d'un modèle" (nouveau projet, nouveau service, nouvelle ressource type), ou qu'on ajoute un nouveau type de modèle au catalogue existant. Contient un exemple complet appliqué à un IDP en Go (Symphony) en fin de document — c'est actuellement le maillon le moins mature de ce projet, à traiter avec prudence.
---

# Scaffolding piloté par configuration déclarative

Beaucoup d'outils ont besoin de générer une nouvelle ressource (projet,
service, composant) à partir d'un modèle réutilisable — un "golden
path", un générateur de boilerplate, un scaffolder. La question de
conception centrale : **le modèle vit-il en dur dans le code, ou dans
une configuration déclarative rechargeable sans recompiler/redéployer
l'outil ?**

Ce skill documente comment raisonner sur cette question, et comment
éviter de deviner un design qui n'a pas été tranché.

## Les deux briques qui doivent se rejoindre, et qui sont souvent confondues

1. **La description d'une ressource déjà existante** (un fichier de
   config qui documente un projet/service déjà créé, généralement ce
   qu'une UI de catalogue affiche).
2. **L'action de création initiale** (le mécanisme qui prend un choix
   utilisateur et produit réellement les fichiers/ressources dans le
   nouveau projet).

Ces deux briques décrivent des moments différents du cycle de vie — la
première est *descriptive et déjà posée*, la seconde est *l'action qui
produit le résultat*. Le pont entre les deux (qu'est-ce qui transforme un
choix utilisateur en fichiers réellement produits, **et** en une entrée
de catalogue mise à jour en conséquence) est souvent la partie la moins
visible et la moins mature d'un projet qui a cette ambition — vérifier
l'existant avant de supposer qu'il est fait.

## Questions à clarifier avant d'implémenter quoi que ce soit ici

Avant de coder cette brique, fais confirmer ces points (par
l'utilisateur ou par lecture du code existant s'il existe déjà) — ne les
suppose jamais :

1. **Où vivent les modèles eux-mêmes ?** Des fichiers gabarits
   organisés par type, chargés dynamiquement par un loader dédié ?
   Vérifier la structure réelle avant de supposer qu'elle existe.
2. **L'action de création met-elle aussi à jour la description
   catalogue correspondante**, ou est-ce une étape séparée après coup ?
   Si la documentation du projet ne le précise pas explicitement, c'est
   une décision de design à trancher avec l'agent d'architecture du
   projet, pas à deviner.
3. **Le mapping type de modèle → ensemble de fichiers à produire**
   est-il en dur dans le code du langage du projet, ou piloté par une
   config déclarative ?** C'est la question structurante : si le projet
   vise une promesse de configuration déclarative (souvent documentée
   dans son architecture comme "ajouter un modèle sans redéployer"),
   coder ce mapping en dur dans le langage du projet contredit
   directement cette promesse.
4. **Comment l'action de création s'articule avec les primitives plus
   bas niveau disponibles** (ex: une fonction qui pousse un seul
   fichier à la fois nécessite-t-elle des appels séquentiels pour
   produire plusieurs fichiers) ? Vérifier l'implémentation réelle
   existante avant d'en supposer une.

## Principe directeur si cette brique est à concevoir/compléter

Si le projet documente un principe de configuration déclarative ou de
"pas de recompilation pour une simple évolution de modèle", un nouveau
modèle de scaffolding devrait être ajoutable/modifiable sans toucher au
code source du projet lui-même — via des fichiers de modèle chargés
dynamiquement, idéalement avec un mécanisme de rechargement à chaud s'il
existe déjà une route ou une commande prévue pour ça.

Si une implémentation proposée code en dur la structure d'un modèle
directement dans le langage du projet (ex: un `switch`/`if-else` géant
avec du contenu de fichier en chaînes littérales dans le code), c'est
probablement une dérive à signaler par rapport à la promesse de
configuration déclarative — surtout si un mécanisme de rechargement à
chaud existe déjà ailleurs dans le projet, ce qui suggère que ce design
en config déclarative était déjà l'intention.

## Garde-fou anti sur-ingénierie spécifique à ce domaine

Le templating/scaffolding est un terrain classique de sur-construction :
la tentation d'un moteur de templating générique, d'un DSL de
configuration extensible "pour tous les cas futurs", ou d'un système de
plugins de modèles, alors qu'un nombre fixe et restreint de types de
modèles suffit largement au besoin réel. Applique le skill
`lazy-minimal-solution` ici en particulier : commence par le nombre de
types de golden path réellement demandés aujourd'hui, pas par une
infrastructure générique anticipant des types hypothétiques.

## Quand passer la main

- Décision de design sur l'architecture du scaffolding elle-même →
  l'agent d'architecture du projet (question structurante, pas un
  détail d'implémentation)
- Implémentation de l'adaptateur de création une fois le design tranché
  → l'agent intégration du projet + skill `adapter-pattern`
- La description catalogue générée et son exposition côté API →
  l'agent API du projet
- L'UI de sélection du modèle → l'agent frontend du projet

---

## Exemple appliqué — Symphony (golden path, IDP en Go)

C'est la fonctionnalité centrale du produit (voir `CLAUDE.md`,
"automatiser les Golden Paths sans introduire de dette de maintenance")
et actuellement son maillon le moins mature. **Ne suppose jamais que
cette partie est terminée ou fonctionne d'une certaine façon sans lire
le code réel d'abord** (`internal/templates/loader.go`, l'implémentation
de `Scaffold()`).

Les deux briques concrètes :
1. `config/services/*.yaml` (ex: `payment-api.yaml`) — décrit un
   service déjà existant, affiché par `Catalogue.vue`.
2. `SCMProvider.Scaffold(repo *RepoResult, cfg ScaffoldConfig)` —
   l'action de création initiale, déclenchée par `Projects.vue`.

Questions ouvertes spécifiques à Symphony, encore à trancher :
- `Scaffold()` génère-t-il aussi le `config/services/*.yaml`
  correspondant, ou est-ce une étape séparée ?
- Le mapping `ScaffoldConfig.Type` → fichiers à pousser est-il en dur en
  Go, ou piloté par une config déclarative — la route
  `POST /api/v1/templates/reload` déjà présente dans `server.go`
  suggère que la deuxième option était l'intention design, à confirmer.
- `Scaffold()` s'appuie-t-il sur des appels séquentiels à `PushFile`
  (qui ne pousse qu'un seul fichier à la fois) ? Vérifier
  l'implémentation réelle dans `gitlab.go` avant d'en supposer une.