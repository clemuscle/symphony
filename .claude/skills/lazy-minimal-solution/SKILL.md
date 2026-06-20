---
name: lazy-minimal-solution
description: Force la solution la plus simple et la plus courte qui résout réellement le problème — questionne si le besoin existe vraiment (YAGNI), privilégie la bibliothèque standard avant du code sur-mesure, les fonctionnalités natives avant une nouvelle dépendance, une ligne avant cinquante. Use PROACTIVELY à chaque écriture de code, et plus encore quand l'utilisateur dit "simplifie", "trop complexe", "yagni", "fais simple", "minimal", ou se plaint de sur-ingénierie, de bloat, de boilerplate ou de dépendances inutiles. Reste actif même en cas de doute — pas de dérive vers la sur-construction au fil d'une session longue.
---

# Solution minimale, posture de dev senior paresseux

Le meilleur code est celui qu'on n'a jamais écrit. "Paresseux" veut dire
efficace, pas négligent — c'est la posture d'un dev senior qui a déjà vu
trop de code sur-conçu et qui a déjà été réveillé à 3h du matin à cause
d'une dépendance qu'on aurait pu éviter.

Ce skill reste actif sur toute tâche d'écriture de code, pas seulement
quand on le demande explicitement — y compris au milieu d'une session
longue où la dérive vers la sur-construction est facile.

## L'échelle de décision, à monter avant d'écrire le moindre code

Avant d'écrire quoi que ce soit, monte cette échelle dans l'ordre — ne
saute pas directement à l'écriture de code :

1. **Ce besoin doit-il même exister ?** (YAGNI — You Aren't Gonna Need
   It). Peut-on supprimer le problème plutôt que le résoudre ? Une
   fonctionnalité demandée "au cas où" sans cas d'usage concret immédiat
   est un candidat à questionner, pas à construire par anticipation.
2. **La bibliothèque standard du langage le fait-elle déjà ?** Avant
   d'écrire une fonction utilitaire, vérifier si elle existe déjà dans
   la stdlib ou dans une dépendance déjà présente dans le projet.
3. **Une fonctionnalité native de la plateforme/du framework le
   fait-elle déjà ?** Avant d'ajouter une dépendance ou un composant,
   vérifier si l'écosystème déjà en place (navigateur, framework,
   runtime) propose nativement l'équivalent. Exemple typique : un champ
   `<input type="date">` natif avant un composant date-picker complet.
4. **Une ligne suffit-elle là où on s'apprêtait à en écrire
   cinquante ?** Préférer la solution la plus directe et la plus courte
   qui résout réellement le problème posé, pas une version généralisée
   pour des cas hypothétiques non demandés.

Ne descends cette échelle vers une solution plus lourde que si l'étage
au-dessus est réellement insuffisant pour le besoin exprimé — pas par
anticipation d'un besoin futur non confirmé.

## Ce que ça veut dire concrètement

- Ne pas créer d'abstraction (interface, factory, couche de
  configuration générique) pour un seul cas d'usage actuel — attendre
  qu'un deuxième cas réel apparaisse avant de généraliser.
- Ne pas ajouter une dépendance pour un besoin que 5-10 lignes de code
  direct couvrent très bien.
- Ne pas construire un système de configuration extensible pour un
  paramètre qui ne varie pas encore en pratique.
- Ne pas anticiper une scalabilité ou des cas limites non demandés —
  les traiter quand ils se présentent réellement, pas en préventif.
- Préférer modifier/supprimer du code existant plutôt que d'empiler une
  nouvelle couche par-dessus si le besoin est de corriger un
  comportement, pas de l'étendre.

## Ce que ça ne veut PAS dire

- Ça ne justifie jamais de sauter une gestion d'erreur réelle, une
  validation de sécurité, ou un test nécessaire — "minimal" porte sur
  la complexité accidentelle (sur-architecture, abstraction prématurée),
  pas sur la rigueur. Un raccourci qui retire de la robustesse n'est pas
  une solution paresseuse, c'est une dette déguisée.
- Ça ne veut pas dire ignorer un pattern déjà établi dans le projet pour
  "faire plus simple" à sa façon — si le projet a déjà une convention
  pour ce type de problème (voir le skill/agent d'architecture du
  projet), la suivre reste la solution la plus simple à long terme,
  même si une solution locale ad hoc semble plus courte dans l'instant.
- Ça ne veut pas dire refuser une complexité réellement nécessaire — si
  le besoin exprimé justifie clairement une abstraction (deuxième cas
  d'usage réel déjà là, pas hypothétique), la construire normalement.

## Niveaux d'intensité

Si le projet ou la personne veut moduler l'agressivité de ce réflexe :
- **Léger** : applique l'échelle de décision mais n'interroge pas les
  demandes explicites déjà précises — exécute-les simplement le plus
  simplement possible.
- **Standard** (par défaut) : applique l'échelle de décision et
  questionne activement les besoins non confirmés ou les abstractions
  prématurées avant de les construire.
- **Strict** : refuse de construire quoi que ce soit qui dépasse le
  besoin immédiat exprimé, même si une extension semble "evidente" —
  demande confirmation avant toute généralisation.

## Quand ce skill doit céder le pas

- Si une convention de projet déjà établie impose un pattern plus
  élaboré pour de bonnes raisons documentées (ex: tout adaptateur doit
  suivre une structure précise pour rester interchangeable — voir
  `adapter-pattern`), suivre cette convention prime sur la solution la
  plus courte dans l'instant.
- Si la simplicité immédiate compromettrait la sécurité, la fiabilité,
  ou un principe d'architecture déjà tranché du projet, ne pas
  sacrifier ces aspects pour réduire des lignes de code.

---

## Exemple appliqué — Symphony (IDP en Go)

Sur Symphony, ce réflexe s'applique particulièrement à deux endroits :

- **Avant d'ajouter un nouveau type de provider ou d'étendre une
  interface** (`internal/providers/interfaces.go`) : est-ce que le
  besoin actuel justifie réellement une nouvelle abstraction, ou un
  appel direct ponctuel suffit pour l'instant ? Voir
  `architecture-guardian` si le doute porte sur une décision
  structurante plutôt qu'un détail d'implémentation.
- **Avant d'ajouter une dépendance Go.** Le projet a déjà un précédent
  de dépendance résiduelle non utilisée (`go-git` présent dans `go.mod`
  sans usage actif dans le driver GitLab actuel, voir `CLAUDE.md`) — un
  rappel concret que les dépendances ajoutées "au cas où" tendent à
  rester comme dette plutôt qu'à servir.

Ce skill ne remplace pas les conventions déjà posées par `adapter-pattern`
ou `architecture-guardian` — il s'applique en amont, sur la question "ce
code/cette dépendance/cette abstraction doit-il exister du tout", avant
même d'arriver à la question "comment le construire dans le style du
projet".