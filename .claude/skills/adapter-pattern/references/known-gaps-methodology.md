# Méthode : repérer les lacunes d'une implémentation de référence sans les reproduire

Une implémentation déjà écrite et fonctionnelle est une référence de
*style*, jamais une référence de *perfection*. Avant de t'en inspirer
pour un nouvel adaptateur, vérifie systématiquement :
- les erreurs de désérialisation/parsing sont-elles vérifiées partout,
  ou silencieusement ignorées à certains endroits ?
- les appels réseau/IO ont-ils une gestion de retry ou d'échec
  transitoire, ou échouent-ils immédiatement sur le moindre incident ?
- une erreur de sous-opération annexe (résolution d'un ID, lookup
  secondaire) est-elle propagée, ou avalée silencieusement en continuant
  avec un résultat partiel ?
- les listes paginées côté API tierce sont-elles vraiment parcourues en
  entier, ou tronquées à une seule page par simplicité ?

Si tu repères un de ces raccourcis dans l'implémentation de référence,
ne le reproduis pas dans le nouvel adaptateur — corrige-le localement, et
propose séparément de corriger l'original si pertinent. Documente la
lacune plutôt que de la laisser se propager silencieusement à chaque
nouvelle implémentation.

L'exemple ci-dessous applique cette méthode au driver GitLab de Symphony.

---

# Exemple appliqué — driver GitLab de Symphony

Le driver GitLab (`internal/providers/scm/gitlab/gitlab.go`) est la
référence de *style*, pas une référence de *perfection*. Ce document liste
ce qui doit être corrigé plutôt qu'imité dans un nouveau driver.

## 1. Erreurs JSON ignorées

`json.Marshal(body)` dans `api()`, `json.Unmarshal(data, &project)` dans
`CreateRepo`, et les unmarshal similaires dans `ListRepos` /
`resolveNamespace` ignorent toutes l'erreur retournée. Conséquence
concrète : si l'API GitLab renvoie un format inattendu (panne partielle,
changement de version d'API), le code continue avec des valeurs zéro
sans jamais le signaler à l'appelant. Un bug silencieux est pire qu'un
crash explicite dans un système qui doit rester auditable.

**Dans tout nouveau driver** : vérifier systématiquement l'erreur de
`json.Unmarshal`.

**Si on retouche `gitlab.go` lui-même** : c'est un bon candidat de
correctif isolé, à proposer à `architecture-guardian` comme petite PR de
fiabilisation plutôt que noyé dans une autre feature.

## 2. Pas de retry/backoff réseau

`p.client.Do(req)` échoue immédiatement sur la moindre erreur réseau
transitoire (timeout, connexion refusée temporairement). Pour des appels
déclenchés par une action utilisateur ponctuelle (créer un repo), c'est
acceptable en MVP — l'utilisateur peut retenter. Pour des appels
périodiques de polling de statut (`GetPipelineStatus`,
`GetPipelineStatus`), l'absence de retry peut faire basculer une tâche en
erreur sur un simple hoquet réseau.

**Recommandation** : pas la priorité du MVP, mais à anticiper pour
`CIProvider.GetPipelineStatus` et tout polling régulier — un retry simple
avec backoff exponentiel borné (2-3 tentatives) évite des faux-négatifs.

## 3. Erreur de résolution de namespace avalée silencieusement

```go
if req.Namespace != "" {
	if nsID, err := p.resolveNamespace(req.Namespace); err == nil {
		payload["namespace_id"] = nsID
	}
}
```

Si `resolveNamespace` échoue (namespace inexistant, typo), le code
continue et crée le repo *sans* le namespace demandé, sans avertir
personne. Un utilisateur qui demande explicitement un namespace et
l'obtient ailleurs (namespace par défaut du token) peut ne jamais s'en
apercevoir avant un audit de sécurité.

**Recommandation** : si `req.Namespace != ""` et que la résolution
échoue, retourner une erreur plutôt que de continuer silencieusement.

## 4. Pagination non gérée

`ListRepos` est codé avec `per_page=50` fixe, sans suivre les pages
suivantes. Au-delà de 50 dépôts accessibles par le token, des dépôts
existants ne remonteront jamais dans `ListRepos()` sans qu'aucune erreur
ne soit levée.

**Recommandation** : pas bloquant tant que le nombre de projets gérés par
Symphony reste faible (MVP), mais documenter cette limite explicitement
quelque part (commentaire dans le code ou `gotchas.md` du projet) pour
qu'elle ne soit pas découverte en production.

## Pourquoi ce document existe

Le but n'est pas de dénigrer le code existant — c'est un driver MVP
fonctionnel et globalement bien structuré. Le but est d'éviter que ces
raccourcis, légitimes une première fois sous pression de livraison,
deviennent un pattern copié-collé dans 4 autres drivers (Harbor, Jenkins,
Kubernetes...) et donc une dette systémique au lieu d'une dette
localisée.
