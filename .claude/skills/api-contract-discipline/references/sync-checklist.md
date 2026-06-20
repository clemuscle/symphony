# Checklist — synchronisation backend/frontend à chaque changement de route

À dérouler chaque fois qu'une route est ajoutée, supprimée ou que son
contrat change (nouveau champ requis, format de réponse modifié).

## Côté backend (internal/api/)

- [ ] La route est déclarée dans `server.go` avec le bon verbe HTTP
- [ ] Le handler valide les inputs avant toute action (jamais de
      confiance aveugle dans le body/params)
- [ ] Le code de statut retourné suit la convention (voir SKILL.md)
- [ ] La forme d'erreur suit le format uniforme du projet
- [ ] Les noms de champs JSON respectent la convention de casse en usage

## Côté frontend (frontend/src/api.js)

- [ ] La fonction correspondante existe dans `api.js`, pas de `fetch()`
      isolé dans un composant
- [ ] Les noms de champs envoyés/attendus correspondent exactement à ce
      que le backend produit/attend (vérifier littéralement, pas de
      mémoire)
- [ ] La gestion d'erreur lit le format uniforme (`error`/`code`) plutôt
      que de catcher une erreur générique sans détail

## Côté composant Vue consommateur

- [ ] Le composant affiche le message d'erreur structuré reçu, pas un
      message générique masquant l'info utile
- [ ] Si la route renvoie un état asynchrone (tâche PENDING/RUNNING), le
      composant reflète visuellement cet état transitoire — voir le
      skill `async-task-state-machine`

## Documentation

- [ ] Si la route est nouvelle, elle est ajoutée à la carte des routes
      dans `SKILL.md` de `api-contract-discipline`
- [ ] Si un breaking change de contrat est introduit sur une route
      existante, vérifier qu'aucun autre composant Vue ne dépend encore
      de l'ancien format avant de livrer
