# Interfaces providers — référence complète

Source de vérité : `internal/providers/interfaces.go`. Ce document
reprend les signatures pour consultation rapide — en cas de divergence,
le fichier Go fait foi, pas ce document.

## SCMProvider

```go
type SCMProvider interface {
	CreateRepo(req RepoRequest) (*RepoResult, error)
	PushFile(projectPath, branch, filePath, content, commitMsg string) error
	ListRepos() ([]Repo, error)
	Scaffold(repo *RepoResult, cfg ScaffoldConfig) error
}
```

- `RepoRequest{Name, Description, Namespace, Private}` — entrée de
  création.
- `RepoResult{ID, Path, WebURL, HTTPCloneURL, SSHCloneURL,
  DefaultBranch}` — sortie normalisée, indépendante de l'outil SCM
  réel.
- `Repo{Name, Path, WebURL}` — forme allégée pour le listing.
- `ScaffoldConfig{Name, Description, Language, Type, Port}` — paramètres
  du golden path à appliquer sur le repo fraîchement créé. Voir le skill
  `declarative-scaffolding` pour comment ça se branche avec
  `config/services/*.yaml` et `internal/templates/loader.go`.

Points d'attention :
- `PushFile` ne prend qu'un seul fichier à la fois. Pour scaffolder
  plusieurs fichiers (cas réel de `Scaffold`), ça implique soit N appels
  séquentiels à `PushFile`, soit une méthode dédiée côté implémentation
  qui boucle dessus — vérifier comment le driver GitLab le fait
  réellement avant de supposer.
- `Scaffold` n'a pas de valeur de retour autre que `error` — si le
  scaffolding produit des informations utiles (ex: liste des fichiers
  créés), elles ne remontent pas via cette interface. À signaler à
  `architecture-guardian` si ce besoin apparaît.

## CIProvider

```go
type CIProvider interface {
	SetupPipeline(projectPath string, pipeline PipelineConfig) error
	TriggerPipeline(projectPath, ref string, vars map[string]string) (string, error)
	GetPipelineStatus(projectPath, pipelineID string) (string, error)
	EnsureRunner(name, executorType string) error
}
```

- `PipelineConfig{Name, Type, Language, Stages}` — config déclarative du
  pipeline à poser sur le repo.
- `TriggerPipeline` retourne un `string` — vraisemblablement l'ID externe
  du pipeline déclenché (à confirmer dans l'implémentation GitLab CI
  réelle). Cet ID est ce qui doit être stocké côté
  `internal/database/pipelines.go` pour permettre un `GetPipelineStatus`
  ultérieur — voir le skill `async-task-state-machine`.
- `GetPipelineStatus` retourne un `string` libre, pas un type énuméré
  Go. La normalisation vers les états internes
  (pending/running/success/failed) doit donc se faire côté appelant
  (probablement dans la couche qui orchestre, pas dans le driver
  lui-même) — vérifier où ce mapping vit avant d'en créer un deuxième
  ailleurs.
- `EnsureRunner` suggère une responsabilité d'auto-provisioning de runner
  CI — à utiliser avec prudence au regard du principe de
  non-intrusivité : s'assurer que ça reste de la config déclarative
  envoyée à l'outil CI, pas un provisioning d'infra direct par Symphony.

## RegistryProvider

```go
type RegistryProvider interface {
	GetRegistryURL(projectPath string) (string, error)
	ListImages(projectPath string) ([]Image, error)
	DeleteImage(projectPath, tag string) error
}
```

- `Image{Name, Tag, Size, CreatedAt}` — `CreatedAt` est un `string`, pas
  un `time.Time`. Vérifier le format retourné par l'API du registre
  (ISO 8601 probable pour GitLab Registry) et le documenter dans
  l'implémentation pour que le frontend sache comment le parser/afficher.

## DeployProvider

```go
type DeployProvider interface {
	Deploy(req DeployRequest) (*DeployResult, error)
	Stop(deploymentID string) error
	Status(deploymentID string) (string, error)
	List() ([]Deployment, error)
}
```

- `DeployRequest{ProjectName, Image, Port, EnvVars, HealthCheck}` —
  `HealthCheck` est un `string`, probablement une URL ou un chemin
  (`/healthz`) à vérifier après déploiement. Confirmer le format attendu
  par le driver Docker actuel avant de le réutiliser pour un futur driver
  Kubernetes (où un healthcheck se modélise différemment — probe HTTP vs
  exec vs TCP).
- Le driver `deploy/docker/docker.go` exécute le déploiement directement
  via le socket Docker local — c'est une exception assumée au principe
  de non-intrusivité, légitime pour un mode "dev local / MVP". Un futur
  `DeployProvider` Kubernetes ne doit PAS suivre ce même schéma
  d'exécution directe : il doit pousser un manifeste et laisser un
  pipeline CI/CD l'appliquer (voir `architecture-guardian` en cas de doute).

## Types ne faisant pas partie d'une interface mais utilisés en commun

Aucun type partagé hors ceux listés ci-dessus n'a été identifié dans le
fichier source au moment de la rédaction de ce skill. Si un nouveau type
commun apparaît (ex: un type d'erreur structuré partagé entre drivers),
documenter son ajout ici.
