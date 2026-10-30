# _(project name)_

_(One paragraph: what this app does, for whom, and what it is not.)_

Built on the [lean-go](https://github.com/tung-dnt/lean-go) template. Architecture and file roles:
[LEANGO.md](LEANGO.md). Quality gates: [CONSTRAINTS.md](CONSTRAINTS.md).

## Implementation recipes

| Change | Guide |
|---|---|
| Domain layout, concern/role filenames and import boundaries | [LEANGO](LEANGO.md) |
| Multi-step state, approval, retries or recovery | [Workflows](docs/conventions/workflow.md) · [`pkg/workflow`](pkg/workflow/workflow.go) |
| Background callbacks, wake/deadline/tick and shutdown | [Background work](docs/conventions/background-work.md) · [`pkg/worker`](pkg/worker/worker.go) |
| Handlers, routing, forms, sessions, SQL, frontend and tests | [Conventions](docs/conventions/) |

Inspect `pkg/` before implementing a reusable mechanism. The template supplies the runtimes, not
an application-specific job queue or persistence adapter; the example note CRUD remains synchronous.

## Getting started

```bash
just install     # Go modules + pinned tools (sqlc, air, esbuild, govulncheck) + the Tailwind CLI + bun packages
just db-up       # start PostgreSQL for `just run` (not needed for `just test`)
just migrate-up  # apply the schema migrations
just run         # build the CSS and JS and run the application on :8000
just admin me@example.com              # create the admin account; the password is printed once
just watch       # run with air, rebuilding on change
just             # every recipe
```

```bash
just test    # go test -race -coverprofile=cover.out ./... (builds main.css and app.js first)
just check   # sqlc-verify, sqlc-vet, lint, vuln, types, secrets, deps, dupes, test, build: what a finished task must pass
```

JavaScript package tools run with Bun as the runtime (`bunx --bun` / `bun x --bun`), not just as packages installed by Bun; avoid bare binaries and Node-based `npx` launches.

## Domains

_(One row per package under `internal/`, and what it owns. Which domain may import which is held by
`internal/architecture_test.go`, not here. Delete `note` when your first domain lands.)_

| Domain | Owns |
|---|---|
| `auth` | login, logout, password hashing of the admin account |
| `note` | the template's example domain |

## Configuration

Configuration is `config/config.yaml`, and every key is overridable as `GO_<SECTION>_<KEY>`
(`GO_HTTP_PORT`, `GO_DATABASE_CONNECTION`, `GO_APP_ENCRYPTIONKEY`, ...). Secrets go in an untracked `.env`
(`cp .env.example .env`): `just` exports every variable in it to the commands it runs. In production, set the same
variables in the deploy platform's environment instead; `.env` is excluded from the Docker build context.

_(The project's own keys and third-party services: name, what it is for, where its secret lives.)_

## Deploying

`Dockerfile` builds a static binary into a `scratch` image (non-root, CA certificates and zoneinfo included) with the
same `just css js` recipes as local development. Run it with `GO_APP_ENVIRONMENT=prod`, `GO_APP_HOST`,
`GO_DATABASE_CONNECTION` and `GO_APP_ENCRYPTIONKEY`; probe readiness at `/healthz`. Behind a reverse proxy, keep the
app's port reachable from the proxy alone: `X-Forwarded-For` is believed only from loopback and private networks
(`middleware.RealIP`).

_(Where it runs, how a release goes out, how the schema is migrated there (`just migrate-up` or the `migrate/migrate`
image, never on startup), and who to call.)_

## Runbooks

_(What to do when it breaks: one heading per alert or known failure.)_

## Decisions

Architecture decisions are in [docs/adr](docs/adr/). Add one per decision that someone would otherwise undo.
