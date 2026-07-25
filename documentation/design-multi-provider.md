# Design memo — multi-provider par catégorie (EPIC C2)

**Statut : livrable de design uniquement. Rien dans ce document n'est
implémenté et rien ne doit être codé à partir de ce document sans repasser
devant `architecture-guardian` d'abord** (décision du fondateur du
2026-07-24, voir `documentation/backlog.md` EPIC C — "design seulement, pas
d'implémentation dans ce cycle").

## 1. Pourquoi ce document

Le retour produit (`documentation/retour.txt`, point 3) demande d'anticiper
plusieurs providers par catégorie (SCM : gitlab + github ; déploiement :
docker + kube + aws), tout en respectant le périmètre MVP documenté dans
`.claude/CLAUDE.md` ("un seul outil par catégorie, ne pas en ajouter un
deuxième avant que le premier soit solide et réellement utilisé"). Ce
document résout cette tension : il ne ferme pas la porte au design, sans
livrer de code qui rouvrirait une décision d'architecture déjà tranchée.

## 2. Constat sur le code actuel

`config/integrations.yaml` porte aujourd'hui **un objet unique par
catégorie** :

```yaml
scm:
    type: gitlab
    url: http://localhost:8929
ci:
    type: gitlabci
    config_repo: symphony-demo/infra
deploy:
    type: docker
    socket: /var/run/docker.sock
```

`internal/drivers/drivers.go:BuildProviderSet` construit **une seule**
instance de chaque catégorie dans un `providers.ProviderSet{SCM, CI,
Registry, Deploy}` global, exposé via `s.getProviders()`
(`internal/api/server.go`). Tous les handlers y accèdent directement
(`pvds.SCM`, `pvds.CI`, ...) sans aucun paramètre de sélection.

**Point important, déjà bien conçu pour ce qui vient après** : le *type* de
driver par catégorie est déjà résolu dynamiquement via des tables
(`scmDrivers`, `ciDrivers`, `registryDrivers`, `deployDrivers` dans
`drivers.go`), conformément à la décision d'architecture n°2 (drivers
compilés, jamais de plugin runtime). Ajouter un nouveau *type* (ex: GitHub,
Kubernetes) ne pose donc aucun problème de principe — ce qui manque est la
capacité d'avoir plusieurs *instances* actives simultanément dans la même
catégorie.

## 3. Schéma cible pour `integrations.yaml`

Chaque catégorie passe d'un objet à une liste d'instances nommées, une
étant marquée par défaut :

```yaml
scm:
  - name: gitlab-primary
    type: gitlab
    url: http://localhost:8929
    default: true
  - name: github-secondary
    type: github
    url: https://github.com

deploy:
  - name: docker-local
    type: docker
    socket: /var/run/docker.sock
    default: true
  - name: k8s-prod
    type: kubernetes
    kubeconfig: /etc/symphony/kubeconfig
```

**Rétrocompatibilité** : le loader de config doit accepter la forme
singulière actuelle (un objet, pas une liste) et la traiter comme une
instance implicite unique nommée `default` avec `default: true` — aucune
migration de `integrations.yaml` existants ne doit être nécessaire tant
qu'un admin n'ajoute pas volontairement une deuxième instance.

**Complication non anticipée par le retour, mais réelle** : les tokens ne
sont jamais lus depuis le YAML (`Token string \`yaml:"-"\`` dans
`SCMConfig`/`CIConfig`/`RegistryConfig`), toujours injectés par variable
d'environnement (`GITLAB_TOKEN`, `SYMPHONY_CI_TOKEN`,
`SYMPHONY_REGISTRY_TOKEN`) via `ApplyEnvOverrides`
(`internal/providers/registry.go`) — un nom de variable fixe par catégorie,
donc valable pour une seule instance. Passer à plusieurs instances par
catégorie casse ce mécanisme de sécurité (section "Sécurité" de
`CLAUDE.md`, "aucun secret en dur") tel qu'il existe aujourd'hui. Il
faudrait un schéma de nommage par instance (ex: `GITLAB_TOKEN__gitlab_primary`)
ou un pointeur vers un coffre-fort par instance — **à trancher avant
d'implémenter**, ce n'est pas un détail.

## 4. Le mécanisme de dispatch — la vraie question de design

Le schéma de config ci-dessus ne dit pas *comment* un projet choisit son
provider. C'est la décision la plus structurante de ce chantier, et ce
document ne la tranche pas — il pose les options.

### Option 1 — Défaut par golden path (recommandée)

`golden-path.yaml` gagne des clés optionnelles, ex.
`spec.scm_provider: gitlab-primary`. À la création, Symphony résout le
provider nommé, ou le provider marqué `default: true` de la catégorie si la
clé est absente.

- **Pour** : prolonge directement le principe déjà en place pour F1
  (`test_tool`/`build_tool` — configuration déclarative par golden path,
  aucun choix supplémentaire imposé au dev dans l'UI). Plus petit blast
  radius d'implémentation.
- **Contre** : un golden path reste lié à un provider fixe ; pas de choix
  à la volée par le dev au moment de la création.

### Option 2 — Choix explicite dans le wizard de création de projet

L'UI de création de projet affiche un sélecteur ("SCM : gitlab-primary ▾")
uniquement quand plusieurs instances existent pour une catégorie (invisible
si une seule — rétrocompatible visuellement avec le flux actuel).

- **Pour** : flexibilité maximale pour le dev.
- **Contre** : complexifie le golden path (étape supplémentaire), risque de
  contredire la promesse "user-friendly" si le dev n'a aucune raison réelle
  de choisir dans la majorité des cas.

### Option 3 — Règle de routage globale (par langage, par équipe...)

Une table de routage admin (ex: `language: python → scm: github-secondary`).

- **Pour** : zéro friction pour le dev.
- **Contre** : nouveau concept ("règles de routage") à concevoir et
  maintenir, surface supplémentaire pour un besoin encore hypothétique.

**Recommandation à valider par le fondateur avant tout code** : Option 1
en premier, avec la possibilité d'ajouter l'Option 2 par-dessus plus tard
si un besoin réel de choix à la volée apparaît — ne pas construire les deux
en même temps (YAGNI).

## 5. Ce que `projects` doit porter

Quelle que soit l'option retenue, le nom du provider utilisé à la création
doit être **persisté sur le projet** (nouvelles colonnes
`scm_provider`, `ci_provider`, `registry_provider`, `deploy_provider` sur
la table `projects`). Sans ça, changer le provider `default: true` d'une
catégorie après coup romprait silencieusement la résolution des actions
futures (trigger pipeline, deploy, list branches...) sur des projets déjà
créés avec un autre provider — un projet doit toujours savoir de façon
stable quel provider l'a créé.

## 6. Inventaire des points du core qui supposent l'unicité aujourd'hui

Chaque site listé résout aujourd'hui "le" provider d'une catégorie sans
paramètre de sélection — tous devront évoluer vers une résolution du type
`providerSetFor(project).X` (ou équivalent) le jour où ceci sera implémenté.

| Catégorie | Fichier | Méthodes appelées | Sites |
|---|---|---|---|
| SCM | `internal/api/projects.go` | `CreateRepo`, `PushFile` (×2), `ListBranches` | 4 |
| SCM | `internal/api/handlers.go` | `ListRepos`, `ListNamespaces` | 2 |
| CI | `internal/api/handlers.go` | `TriggerPipeline` (générique), `GetPipelineStatus`, destroy-recette | 3 |
| CI | `internal/api/projects.go` | `SetProjectVariable`, `TriggerPipeline` (×3 : deploy / createRecette / destroy) | 4 |
| Registry | `internal/api/projects.go` | `GetRegistryURL`, `ListImages` | 2 |
| Deploy | `internal/api/inventory.go` | `ListContainers` | 1 |

**Total : ~16 sites d'appel**, plus les changements structurels suivants :

- `internal/drivers/drivers.go:BuildProviderSet` → `BuildProviderSets`,
  bouclant sur les listes de config. Comportement à définir : un ping raté
  sur une instance *secondaire* doit-il bloquer tout le démarrage (comme
  aujourd'hui pour l'instance unique), ou seulement désactiver cette
  instance ? Aujourd'hui, un SCM injoignable met tout Symphony en "mode
  setup" — ce comportement devient trop strict dès qu'il y a plusieurs
  instances.
- `s.pvds *providers.ProviderSet` (`internal/api/server.go`) devient une
  structure indexée par catégorie + nom plutôt qu'un pointeur unique.
- `internal/api/setup.go` (`getSetupStatus`, `saveSetup`, `testProvider`) et
  `frontend/src/views/Setup.vue` : le wizard passe d'un formulaire par
  catégorie à une gestion de liste (ajouter/supprimer une instance,
  désigner le défaut). `AvailableTypes()` reste inchangé (types de driver
  compilés, pas instances).
- Golden paths : nouvelles clés optionnelles `spec.*_provider` si Option 1
  est retenue (§4).

## 7. Effort estimé (ordre de grandeur, pas un chiffrage engagé)

| Chantier | Taille |
|---|---|
| Schéma config + rétrocompatibilité singulier→liste | S |
| Nommage des tokens par instance (sécurité, §3) | S — mais bloquant, à trancher avant tout le reste |
| Colonnes `projects` + résolution du dispatch (Option 1) | S–M |
| Migration des ~16 sites d'appel + `BuildProviderSets` | M |
| Wizard UI (liste d'instances, sélection du défaut) | M |
| Tests de non-régression (mono-provider actuel) + nouveaux tests multi | M |

Chantier de taille moyenne (plusieurs jours), pas un ticket isolé — à
traiter comme un epic à part entière, avec passage devant
`architecture-guardian` avant le premier commit de code, conformément à la
décision du fondateur.

## 8. Ce que ce document ne fait pas

- Il ne tranche pas quelle option de dispatch (§4) retenir — décision
  produit du fondateur, pas de ce document.
- Il ne code rien — le MVP reste mono-provider tel que documenté dans
  `CLAUDE.md`.
- Il ne débloque pas A4 (import d'un projet déjà existant ailleurs, voir
  `backlog.md` EPIC A) — qui reste différé indépendamment de C2, et qui
  soulève ses propres questions (mode "adopter" vs "provisionner") au-delà
  du seul multi-provider.
