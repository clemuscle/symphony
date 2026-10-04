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
go test ./...                                   # tests unitaires (seul handlers/catalogue_test.go pour l'instant)
go test ./internal/api/handlers -run TestResolveInputs
```

## Runtime dependencies

- **`symphony.yaml`** at the repo root (override the path with `SYMPHONY_CONFIG`). `${VAR}` references in it are expanded with `os.ExpandEnv` before parsing.
- **`.env`** at the repo root, loaded by godotenv. If it's missing, the server logs a warning and uses the process environment.
- **PostgreSQL**, e.g. the local `symphony-postgres` container, configured via `DB_*` env vars.
- **GitLab** at `http://gitlab.local:8929` (the URL comes from `symphony.yaml`). The default SCM provider must work at startup, because the catalogue syncer and the template loader both use it. The process exits if no default SCM provider is registered.
- **Docker socket** at `/var/run/docker.sock`, set per runtime provider in `symphony.yaml`.

SQL migrations are embedded in the binary with `go:embed`. `symphony.yaml` and `.env` are still read from the working directory. The `Dockerfile` still uses `golang:1.22-alpine`, while go.mod requires Go 1.25.

### Environment variables

| Variable | Used by | Default |
|---|---|---|
| `GITLAB_TOKEN` | GitLab SCM (`token_env`), and the GitLab CI fallback | — |
| `GITHUB_TOKEN` | GitHub SCM and GitHub Actions (`token_env`) | — |
| `DB_HOST` / `DB_PORT` / `DB_NAME` / `DB_USER` / `DB_PASSWORD` | `database.Connect` | `localhost` / `5432` / `symphony` / `symphony` / `symphony123` |
| `PORT` | HTTP server | `8080` |
| `SYMPHONY_CONFIG` | config path | `symphony.yaml` |
| `CATALOGUE_BRANCH` | catalogue syncer branch | `main` |
| `TEMPLATES_REPO` | fallback templates repo, used when a golden path has no `repo` | `catalogue.repo` |

`catalogue.branch` in `symphony.yaml` is **ignored**. The branch only comes from `CATALOGUE_BRANCH`.

## Architecture

```
cmd/symphony/main.go        composition root: loads config, reads the providers section of symphony.yaml and registers each one
internal/config/            symphony.yaml types + loader (providers, catalogue, golden_paths)
internal/provider/          interfaces.go (SCM, CI, Registry, Runtime) + registry.go (the provider Registry) + implementations:
  scm/gitlab, scm/github
  ci/gitlabci, ci/githubactions
  runtime/docker            Docker Engine API over the unix socket (no SDK)
  runtime/kubernetes        stub: every method returns "non implémenté"
internal/catalogue/         in-memory Store + Service types
internal/gitops/            syncer.go (catalogue poll) + template_loader.go (golden paths, scaffold/CI fetch)
internal/database/          database/sql + lib/pq, no ORM; schema in migrations/001_init.sql
internal/api/               server.go (chi router, all routes, /healthz) + handlers/ (one file per domain)
internal/auth/              empty: OIDC not implemented
frontend/                   Vue 3 + Vite + vue-router + axios
```

**Provider wiring**: `main.go` loops over `cfg.Providers.{SCM,CI,Runtime}` and switches on `type` (`gitlab`, `github`, `gitlab_ci`, `github_actions`, `docker`, `kubernetes`). It creates each provider and calls `providers.Register*(p, def.Default)`. To add a provider type, implement the interface in `internal/provider/interfaces.go` and add a `case` in `main.go`. Handlers look providers up by id with `providers.SCM(id)`, `CI(id)` or `Runtime(id)`. An empty id returns the default one.

Two different things are both called "registry", so don't mix them up:
- `provider.Registry` (`registry.go`) is the container of all providers.
- `RegistryProvider` is the *container-image* registry interface. It has **no implementation**, and `main.go` never registers one, even though `config.ProvidersConfig` has a `Registry` section and golden paths have `registry_provider`. `providers.GetRegistry()` will always error.

Tokens are never written in `symphony.yaml`. Each provider has `config.token_env` naming an env var. GitLab SCM falls back to a literal `config.token`. GitLab CI tries `token_env`, then `<scm_ref>_TOKEN`, then `GITLAB_TOKEN`.

**Service catalogue (GitOps)**: the catalogue is loaded from **GitLab** (`catalogue.repo`/`catalogue.path`, by default `root/symphony-config` → `services/`) by `gitops.Syncer`, through the default SCM provider. It polls every 15s and replaces the in-memory `catalogue.Store`, and `POST /api/v1/sync` forces a reload. It is **not** loaded from the local `config/` directory (`config/services/*.yaml` and `config/integrations.yaml` are leftovers, unused). The catalogue is not stored in Postgres.

**Golden paths**: they are defined in `symphony.yaml` under `golden_paths`, not fetched from GitLab. Each one names its SCM/CI/runtime provider ids and points to a `scaffold` directory and a `ci_template` file in `root/symphony-templates` on GitLab (`source: git`, the only supported source). `TemplateLoader` fetches those files through the SCM provider when a project is created.

**Project creation** (`handlers/projects.go`):
1. Resolve the golden path by id, then its SCM and CI providers.
2. `CreateRepo`.
3. Fetch the scaffold files (recursively) and the CI template, replace placeholders. The CI template becomes `.gitlab-ci.yml` (the file name is hardcoded, even for GitHub).
4. `PushFiles` everything in **one** commit. A first commit without `.gitlab-ci.yml` would trigger GitLab Auto DevOps.
5. Save the project in `projects`.
6. `syncer.Register` writes `services/<name>.yaml` to the catalogue repo and forces a sync. A failure there doesn't undo the creation; it is logged and returned as `catalogue_error`. (The CI templates no longer have a `register-service` job.)
7. Write an audit log entry.

The CI provider is resolved but not used yet (`_ = ci`).

**Persistence**: tables `projects`, `pipelines`, `deployments`, `audit_log` and `schema_migrations`. `Migrate()` applies each migration listed in `db.go` whose version is missing from `schema_migrations`. To add a migration, create `migrations/00N_*.sql`, add a `//go:embed` variable for it, **and** add it to the list in `db.go`. Mutating handlers record an audit entry with `db.LogAudit(action, resource, details)`.

**API ↔ frontend JSON shapes**: every struct the API serializes or decodes has explicit snake_case `json` tags (`catalogue.*`, `config.GoldenPath`, `provider.Repo/Pipeline/Deployment`, `database.*`, request bodies). The frontend reads exactly those keys. Any new API struct needs json tags, otherwise it would serialize with Go field names.

Every call in `frontend/src/api.js` has a matching route in `server.go`. `POST /services/{name}/actions/{action}` (`handlers.TriggerAction`) runs a catalogue action: a POST to `webhook_url`, or a pipeline on the service's `spec.repo` through `ci_provider` (inputs become CI variables, plus `SYMPHONY_SERVICE` / `SYMPHONY_ACTION` / `SYMPHONY_PIPELINE`). An action with neither returns 400. `resolveInputs` validates the body against the declared inputs (defaults, required, integer bounds, options) and **rejects undeclared keys**, so callers can't inject arbitrary CI variables. Services created by Symphony get a default "Relancer la pipeline" action.

**Project deletion** (`DELETE /projects/{name}`) runs in the background and returns 202, because GitLab refuses to delete a project until its container registry purge (asynchronous, often 2–3 min) completes. Order: `DeleteRepo` (purges the registry, then retries), `syncer.Unregister`, then the DB rows (project and its pipelines; the audit log is kept). While it runs, `GET /projects` returns `deleting: true` (an in-memory flag, lost on restart), and a second DELETE returns 409.

Golden paths come from `symphony.yaml` and are only loaded at startup; there is no hot reload.

The frontend base URL is hardcoded to `http://localhost:8080` in `frontend/src/api.js`.
