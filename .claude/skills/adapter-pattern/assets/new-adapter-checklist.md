# Checklist — nouvel adaptateur (exemple Symphony)

Gabarit générique de checklist pour tout nouvel adaptateur derrière une
interface de ce projet ; les libellés ci-dessous utilisent le
vocabulaire Go/Symphony ("driver", `interfaces.go`) à titre d'exemple —
adapte les termes au langage/projet réel si différent.

À cocher avant de considérer un adaptateur prêt à être intégré.

## Conformité au contrat

- [ ] Implémente l'interface complète sans méthode manquante
- [ ] Aucune signature de méthode modifiée par rapport à `interfaces.go`
- [ ] `var _ providers.XxxProvider = (*Provider)(nil)` présent (vérif
      compile-time)

## Structure et style

- [ ] Struct minimale, champs de connexion clairs
- [ ] `New(...)` normalise les entrées (trim des URLs notamment)
- [ ] Un seul point d'appel HTTP centralisé (`api(...)` ou équivalent)
- [ ] Timeout HTTP explicite présent

## Robustesse

- [ ] Toutes les erreurs de `json.Marshal`/`json.Unmarshal` sont
      vérifiées (pas de `_` qui avale une erreur de parsing)
- [ ] Toutes les erreurs sont préfixées par `<driver> <opération>:`
- [ ] Le status code de succès attendu est vérifié précisément, pas en
      plage large
- [ ] Aucune erreur de sous-appel (ex: résolution d'un ID annexe) n'est
      avalée silencieusement sans impact documenté sur le résultat

## Architecture (principes du PID)

- [ ] Aucun appel direct d'exécution d'infra (pas de `terraform apply`,
      `kubectl apply` direct, etc.) sauf exception déjà actée et
      documentée (ex: `docker_local` MVP)
- [ ] Scope des credentials minimal — documenté dans le commentaire du
      package ou la doc d'installation
- [ ] Pas de dépendance lourde ajoutée si un appel REST simple suffisait

## Intégration

- [ ] La clé `provider:` correspondante dans `config/integrations.yaml`
      est cohérente avec le nom du package/driver
- [ ] Le mapping clé YAML → implémentation est fait à un seul endroit
      (pas de switch dupliqué)
- [ ] Testé manuellement contre une instance réelle (ou mock) de l'outil
      cible, pas seulement compilé

## Documentation

- [ ] Limites connues du driver documentées (pagination, rate limit,
      retry absent...) si applicable
- [ ] Si le driver diverge intentionnellement du pattern GitLab de
      référence, la raison est commentée dans le code
