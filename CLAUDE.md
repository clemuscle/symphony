# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

Symphony is an Internal Developer Platform (IDP): a Go backend with a Vue 3 SPA frontend that sits on top of SCM/CI providers (self-hosted GitLab by default, GitHub optional) and a container runtime (local Docker). It creates repos from "golden paths", pushes scaffold files and a CI pipeline, deploys containers, and shows a service catalogue. `documentation/gemini.txt` is the (French) architecture proposal it is built from ("KoreIDP"). Parts of it (OIDC/RBAC, Vault, K8s, `/metrics`, an embedded SPA) are **not implemented**.

Code comments, log messages and API error strings are in French. Keep that convention.

## Commands

```bash
go build ./...                 # build (use -gcflags=-e to see all errors, not just the first 10 per package)
go vet ./...
go run ./cmd/symphony          # run the server from the repo root (see "Runtime dependencies")
cd frontend && npm run dev     # Vue 3 + Vite dev server (npm install first)
```

```bash
go test ./...                                   # tests unitaires (handlers, provider, gitlabregistry, docker)
go test ./internal/api/handlers -run TestResolveInputs
```

## Runtime dependencies

- **`symphony.yaml`** at the repo root (override the path with `SYMPHONY_CONFIG`). `${VAR}` references in it are expanded with `os.ExpandEnv` before parsing.
- **`.env`** at the repo root, loaded by godotenv. If it's missing, the server logs a warning and uses the process environment.
- **PostgreSQL**, e.g. the local `symphony-postgres` container, configured via `DB_*` env vars.
- **GitLab** at `http://gitlab.local:8929` (the URL comes from `symphony.yaml`). The default SCM provider must work at startup, because the catalogue syncer and the template loader both use it. The process exits if no default SCM provider is registered.
- **Docker socket** at `/var/run/docker.sock`, set per runtime provider in `symphony.yaml`.

SQL migrations are embedded in the binary with `go:embed`. `symphony.yaml` and `.env` are still read from the working directory.

### Environment variables

| Variable | Used by | Default |
|---|---|---|
| `GITLAB_TOKEN` | GitLab SCM (`token_env`), and the GitLab CI fallback | — |
| `GITHUB_TOKEN` | GitHub SCM and GitHub Actions (`token_env`) | — |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | `database.Connect` | `localhost` / `5432` / `symphony` / `symphony` / `symphony123` |
| `PORT` | HTTP server | `8080` |
| `SYMPHONY_CONFIG` | config path | `symphony.yaml` |
| `CATALOGUE_BRANCH` | catalogue syncer branch | `main` |

`catalogue.branch` in `symphony.yaml` is **ignored**. The branch only comes from `CATALOGUE_BRANCH`.

## Architecture

```
cmd/symphony/main.go        composition root: loads config, reads the providers section of symphony.yaml and registers each one
internal/config/            symphony.yaml types + loader (providers, catalogue, templates repo)
internal/provider/          interfaces.go (SCM, CI, Registry, Runtime) + registry.go (the provider Registry) + implementations:
  scm/gitlab, scm/github
  ci/gitlabci, ci/githubactions
  registry/gitlabregistry   GitLab container registry (image URL, tags)
  runtime/docker            Docker Engine API over the unix socket (no SDK). Deploy pulls the image first (registry auth via X-Registry-Auth), then replaces the container
  runtime/kubernetes        stub: every method returns "non implémenté"
internal/catalogue/         in-memory Store + Service types
internal/gitops/            syncer.go (catalogue poll) + template_loader.go (golden paths poll, skeleton/CI fetch)
internal/database/          database/sql + lib/pq, no ORM; schema in migrations/001_init.sql
internal/api/               server.go (chi router, all routes, /healthz) + handlers/ (one file per domain)
internal/auth/              empty: OIDC not implemented
frontend/                   Vue 3 + Vite + vue-router + axios
```

**Provider wiring**: `main.go` loops over `cfg.Providers.{SCM,CI,Registry,Runtime}` and switches on `type` (`gitlab`, `github`, `gitlab_ci`, `github_actions`, `gitlab_registry`, `docker`, `kubernetes`). It creates each provider and calls `providers.Register*(p, def.Default)`. To add a provider type, implement the interface in `internal/provider/interfaces.go` and add a `case` in `main.go`. Handlers look providers up by id with `providers.SCM(id)`, `CI(id)` or `Runtime(id)`. An empty id returns the default one.

Two different things are both called "registry", so don't mix them up:
- `provider.Registry` (`registry.go`) is the container of all providers.
- `RegistryProvider` is the *container-image* registry interface, looked up with `providers.GetRegistry(id)`. Its only implementation is `registry/gitlabregistry` (`type: gitlab_registry`, `config.registry_host`, e.g. `gitlab.local:5050`). Its `GetImageURL` returns `<registry_host>/<repo path in lowercase>`. Its `ListTags` lists the tags of the project's root image (not of sub-images), with `latest` first.

At creation, the project stores the resolved registry id and its image URL (`reg_provider`, `registry_url`). The registry is optional, so a missing one doesn't block creation. `Deploy` passes the registry's `Auth()` to the runtime, but only when the image is hosted on that registry (prefix `ServerAddress/`), so the token never goes to a third-party registry. The Docker runtime pulls **before** stopping the old container: if the pull fails, the running deployment keeps running. If an image already exists locally (a local build, or an unreachable registry), Docker uses it and logs a warning. The Docker daemon must list `gitlab.local:5050` as an insecure registry (HTTP).

Ports: `port` in a deploy request is the port the app listens on **inside** the container. `host_port` is the published port. When it's 0, Docker reuses the host port of the container being replaced (so the URL stays stable), or picks a free port on a first deployment. The returned `Deployment.Port`/`URL` are the host port actually published, read by inspecting the container after it starts.

Replacing a container: the new one is created next to the old one, as `<name>-next`. The old one is stopped (which frees the host port) but kept. The new one must still be `running` after `startupGrace` (3 s). If it is, the old one is deleted and the new one renamed to `<name>`. Otherwise (port taken, crash at startup), the new one is deleted, the old one is restarted, and the error includes the last lines of the new container's logs. The service is still down for a few seconds during the switch, since both containers would bind the same host port. `Logs()` strips the 8-byte frame header that Docker adds to each line.

`GET /projects/{name}/image` returns `image`, `tags`, `port` and `runtime_provider`, and the Deployments form uses them to prefill a deployment.

Tokens are never written in `symphony.yaml`. Each provider has `config.token_env` naming an env var. GitLab SCM falls back to a literal `config.token`. GitLab CI and the GitLab registry try `token_env`, then `<scm_ref>_TOKEN`, then `GITLAB_TOKEN` (`gitlabToken` in `main.go`).

**Service catalogue (GitOps)**: the catalogue is loaded from **GitLab** (`catalogue.repo`/`catalogue.path`, by default `root/symphony-config` → `services/`) by `gitops.Syncer`, through the default SCM provider. It polls every 15s and replaces the in-memory `catalogue.Store`, and `POST /api/v1/sync` forces a reload (of the golden paths too). It is **not** loaded from the local `config/` directory (`config/services/*.yaml` and `config/integrations.yaml` are leftovers, unused). The catalogue is not stored in Postgres.

**Golden paths**: they are loaded from the **templates repo** (`templates.repo`/`branch`/`path` in `symphony.yaml`, by default `root/symphony-templates` → `templates/`) by `gitops.TemplateLoader`, through the default SCM provider, and reloaded every 15s like the catalogue: adding a golden path means pushing a folder. Each folder `templates/<name>/` holds a `template.yaml` (`kind: GoldenPath`, `metadata.name` must equal the folder name, `spec.providers.{scm,ci,registry,runtime}` with empty = default, `spec.skeleton` (default `skeleton`) and `spec.ci_template`, both relative to the folder and unable to escape it), the CI template and the skeleton. The format is documented in that repo's README. A template that fails to parse (unknown keys are rejected), misses its files, or names an unknown or incompatible provider is skipped with a warning, and the others stay available. If the repo can't be read, the previous list is kept. Warnings are only logged when the result changes. `GET /golden-paths` returns `gitops.GoldenPath` (same JSON keys as before, `skeleton`/`ci_template` are full repo paths). The Projects page has a "Recharger depuis Git" button that calls `POST /sync`.

**Project creation** (`handlers/projects.go`):
1. Resolve the golden path by id, then its SCM provider (the request's `scm_provider` can override it) and its CI provider. `provider.CheckCompatible` rejects with a 400 a pair whose `Platform()` differs (e.g. a GitHub repo with GitLab CI); the loader already skips golden paths with an incompatible pair.
2. Fetch the skeleton files (recursively) and the CI template, replace placeholders, **before** creating the repo, so a broken template doesn't leave an empty repo. The CI template is written at `ci.ConfigFile()` (`.gitlab-ci.yml` for GitLab CI, `.github/workflows/ci.yml` for GitHub Actions).
3. `CreateRepo`.
4. `PushFiles` everything in **one** commit. That push starts the first pipeline (Symphony doesn't trigger one itself); on GitLab a first commit without `.gitlab-ci.yml` would trigger Auto DevOps.
5. Save the project in `projects`.
6. `syncer.Register` writes `services/<name>.yaml` to the catalogue repo and forces a sync. A failure there doesn't undo the creation; it is logged and returned as `catalogue_error`. (The CI templates no longer have a `register-service` job.)
7. Write an audit log entry.

The pipelines links (API response and catalogue entry) come from `ci.PipelinesURL`. GitHub Actions `TriggerPipeline` takes `owner/repo/workflow.yml`, or `owner/repo` for Symphony's `ci.yml`; the workflow needs `workflow_dispatch` with the inputs declared, since GitHub rejects undeclared inputs (including `SYMPHONY_*`).

**Persistence**: tables `projects`, `pipelines`, `deployments`, `audit_log` and `schema_migrations`. `Migrate()` applies each migration listed in `db.go` whose version is missing from `schema_migrations`. To add a migration, create `migrations/00N_*.sql`, add a `//go:embed` variable for it, **and** add it to the list in `db.go`. Mutating handlers record an audit entry with `db.LogAudit(action, resource, details)`.

**API ↔ frontend JSON shapes**: every struct the API serializes or decodes has explicit snake_case `json` tags (`catalogue.*`, `gitops.GoldenPath`, `provider.Repo/Pipeline/Deployment`, `database.*`, request bodies). The frontend reads exactly those keys. Any new API struct needs json tags, otherwise it would serialize with Go field names.

Every call in `frontend/src/api.js` has a matching route in `server.go`. `POST /services/{name}/actions/{action}` (`handlers.TriggerAction`) runs a catalogue action: a POST to `webhook_url`, or a pipeline on the service's `spec.repo` through `ci_provider` (inputs become CI variables, plus `SYMPHONY_SERVICE` / `SYMPHONY_ACTION` / `SYMPHONY_PIPELINE`). An action with neither returns 400. `resolveInputs` validates the body against the declared inputs (defaults, required, integer bounds, options) and **rejects undeclared keys**, so callers can't inject arbitrary CI variables. Services created by Symphony get a default "Relancer la pipeline" action.

**Project deletion** (`DELETE /projects/{name}`) runs in the background and returns 202, because GitLab refuses to delete a project until its container registry purge (asynchronous, often 2–3 min) completes. Order: `DeleteRepo` (purges the registry, then retries), `syncer.Unregister`, then the DB rows (project and its pipelines; the audit log is kept). While it runs, `GET /projects` returns `deleting: true` (an in-memory flag, lost on restart), and a second DELETE returns 409.

Providers come from `symphony.yaml` and are only loaded at startup; there is no hot reload of providers.

The frontend API base URL comes from `VITE_API_URL` at build time (e.g. in `frontend/.env.local`, gitignored), with `http://localhost:8080` as the default. An empty value means same origin. The server sends `Access-Control-Allow-Origin: *`.
