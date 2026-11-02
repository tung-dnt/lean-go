# LEANGO: the lean-go architecture

A template for server-rendered Go apps: `net/http`, PostgreSQL through sqlc, gomponents,
htmx, Alpine, Tailwind and daisyUI. No router framework, ORM, DI container or `init()` registration.
Application-specific behavior belongs in [README.md](README.md) and feature guides, not here.
The quality bar is [CONSTRAINTS.md](CONSTRAINTS.md); implementation recipes are in
[docs/conventions](docs/conventions/), selected by [AGENTS.md](AGENTS.md).

## Start a project

**Do** use GitHub's **Use this template**, clone the new repository, then:

1. Rename the Go module and its Go import paths from `github.com/tung-dnt/lean-go` to your module.
   Update the `module` constant in `internal/architecture_test.go` too.
2. Rename `leango` in local database settings, Docker Compose and `package.json` to your app name.
3. Run `just install && just db-up && just migrate-up && just run`; open <http://localhost:8000>.
4. Copy the shape of `internal/note` for the first real domain, then remove the example's package,
   routes, paths, migration, queries, architecture-test row and `web/js/note` area together.
5. Set the owner/date in `CONSTRAINTS.md` and fill in the README's project-specific sections.
6. Set `GO_APP_ENCRYPTIONKEY` and database credentials in every live environment.

**Don't** ship the public key or local credentials from `config/config.yaml`, keep two competing
module paths, or leave example routes available just because nothing links to them.

## Package boundaries

```text
cmd/web/                 entrypoint: calls server.Run
cmd/admin/               create the admin account
internal/server/         config, concrete wiring, routes, middleware, shutdown
internal/server/paths/   typed paths used by links, forms and redirects
internal/server/servertest/ real server test harness
internal/ui/             page shell, layouts and shared components
internal/ui/pages/       every full page, one `<url>_page.go` file each
internal/shared/<concern>/ code two or more domains share, one subpackage per concern
internal/<domain>/       one flat package per domain
internal/external/db/    migrations, SQL queries, generated sqlc code
pkg/                     domain-independent mechanisms, including workflow and worker
web/js/                  browser areas with explicit import edges
public/                  embedded assets
```

```mermaid
flowchart TD
  cmd["cmd/*"] --> server["internal/server"]
  server --> domain["internal/<domain>"]
  server --> ui["internal/ui"]
  server --> external["internal/external"]
  server --> pkg["pkg"]
  domain --> ui
  domain --> shared["internal/shared/<concern>"]
  domain --> external
  ui --> shared
  domain --> paths["internal/server/paths"]
  domain --> pkg
  ui --> paths
  ui --> pkg
  external --> pkg
```

**Do** inspect `pkg/` before implementing a mechanism. Read its contract and callers and reuse
what fits. Keep third-party SDK adapters under `internal/external`; generated sqlc files are never
hand-edited. A domain imports no other domain: extract what two domains share into
`internal/shared/<concern>` (rule R2, "Flat domains"). Domains link to each other's pages through
typed paths instead of importing each other's handlers.

**Don't** import a domain, server or UI from `pkg` or `internal/external`. UI imports no domain and
only `internal/server/paths` from server; paths imports nothing from the module. `internal/ui/pages`
also imports nothing of `internal/external`: a page takes plain parameters. `internal/shared`
imports no domain, server, UI or external, and `pkg` never imports it. `cmd/*` imports
server, pkg and external plus explicitly listed `cmdAllowed` edges (`cmd/admin` uses auth).
The architecture test enforces these edges and rejects the router/validation libraries replaced
by the standard library.

## Flat domains, named by concern and role

**Do** keep related functions in one package and name files so a reader can locate a concern without
opening every file. One example domain, including an optional workflow's file layout:

```text
internal/note/
  note_http.go
  note_test.go
  publication_http.go
  publication_views.go
  publication_form.go
  publication_store.go
  publication_worker.go
  publication_workflow_definition.go
  publication_workflow_prepare.go
  publication_workflow_publish.go
  publication_workflow_error.go
```

Only `note_http.go` and `note_test.go` are supplied in this example domain; its pages are
`internal/ui/pages/home_page.go` and `notes_id_page.go`, and the login page is `login_page.go`. A
concern with fragments or answers to a post adds a `_views.go` file.
The `publication_*` entries show where a real multi-step concern would go; simple note CRUD
needs neither a workflow nor a background worker.

Every non-test `.go` file directly in `internal/<domain>` is either `<pkg>.go` or
`<concern>_<role>.go`, with a role from this closed list:

| File pattern | Owns |
|---|---|
| `<concern>_http.go` | One file handles an incoming request: the controller and all its processing logic (reads, decisions, entrypoints) together |
| `<concern>_views.go` | That concern's fragments, answers to a POST/action, SSE frames and JSON; a full page is a template in `internal/ui/pages` |
| `<concern>_form.go` | Reading and validating request input into typed values |
| `<concern>_store.go` | SQL/transaction adapter and persistence invariants, only for persistence used by more than one role (HTTP, worker, workflow step, command) |
| `<concern>_worker.go` | Domain callbacks, durable scanning and scheduling policy, with the logic only the worker uses |
| `<concern>_effects.go` | Durable external-effect handling: the outbox of post-commit work (storage cleanup, deliveries) and its drainer |
| `<concern>_test_support.go` | Exported test seams in production code, used only by the package's `test/` folder; the application never calls them |
| `<concern>_workflow_definition.go` | Complete graph, states, guards, retry/error policy and callback wiring |
| `<concern>_workflow_<step>.go` | One step's replay-safe Do and transactional Commit, kept together |
| `<pkg>.go` | Only the package doc, types, constants and sentinel errors shared by two or more concerns, and methods on those types |

`<pkg>.go` carries the package's own name (`note.go` in `internal/note`); it holds no function other
than methods on its types: no handler, query or SQL. `internal/shared/<concern>` and
`internal/external/<system>` are not held to this list: no request is handled there. The architecture
test enforces the file names; the content rule is a review rule.

**Place each function in exactly one file, by its callers.** Used by one `_http.go` file only: into
that file. Used only by one workflow step, worker or effects file: into that file. Persistence used by
two or more roles: `<concern>_store.go`. Input validation used by several handlers:
`<concern>_form.go`. Pure logic used by two or more roles: the file of the role that owns the concept.
A function with no home by these rules is a stop-and-ask case, like a missing role.

**Do** stop and ask the user when no role fits a file. The list is closed: never invent a suffix
(`_service`, `_types`, `_utils`, `_input`, ...). Adding a role changes this table and
`domainFile` in `internal/architecture_test.go` together, after the user agrees.

**R1: no layers.** The `<concern>_http.go` file is the controller and calls domain functions directly.
Never add a `_service.go`, `_controller.go` or any layer that only forwards calls. There is no `_api`,
`_query` or `_policy` file: one file handles a request, its processing logic stays beside its controller.

**R2: no domain-to-domain imports.** A domain imports no other domain. Code that two or more domains
share lives in `internal/shared/<concern>`: one subpackage per concern, never a flat `common` or
`util` bucket. Shared views go to `internal/ui/components`. Code with a single consumer stays with
that consumer. `internal/shared` imports no domain, `internal/server`, `internal/ui` or
`internal/external`, and `pkg` imports no `internal/shared`.

A second workflow uses another concern prefix in the same flat domain. Not every role needs a file:
keep small cohesive code together and split along a real role, not a line-count target.

**Don't** introduce `workflows/<name>/`, `handlers/` or `services/` subpackages to organize one domain,
put every view into `views.go`, or add a service/interface layer that only forwards calls.

### Full pages: `internal/ui/pages`

**Do** draw every full HTML page (a URL a browser navigates to, rendered in the layout) as a render
function in the one flat package `internal/ui/pages`, in one file named by its URL path: the path's
segments joined by `_`, a `{x}` wildcard written as lower-case `x`, and the suffix `_page.go`; the path `/`
is `home_page.go`, `/notes/{id}` is `notes_id_page.go`, `/login` is `login_page.go`. Only `_page.go` files
(and `test/`) live there, and the architecture test refuses any other file.

A page is a template: it takes its data as plain function parameters (built-ins, `internal/ui/components`
types, `internal/shared/*` types, `pkg/*`) and imports no domain and nothing of `internal/external`. The
domain's `<concern>_http.go` handler is the orchestrator: it queries, maps what it loaded to the page's
parameters, calls the page and chooses the layout (`ui.Render(w, page, layouts.Primary, pages.HomePage(...))`).
A page the handler also answers a failed post with (the list with the form and its problems) is the same page.

Fragments, answers to a POST/action, SSE frames and JSON stay in the domain's `<concern>_views.go`.
Markup that a page and a fragment both draw moves to `internal/ui/components`, so that the page and the domain
never import each other. A handler is the only caller of a page.

**Don't** put a handler, a query, a form type or a fragment in `internal/ui/pages`, give a page a domain
or `db` type to draw, or add a second page package. A file with no `_page.go` name has no home there: stop
and ask the user.

**Read a flow in this order:** entrypoint → `<concern>_workflow_definition.go` → every referenced
step/guard/error callback → store → worker/HTTP caller. The definition must expose the whole graph;
flat files are discoverable roles, not permission to read one file and guess the rest.

## Choose workflow and worker separately

**Do** use [`pkg/workflow`](pkg/workflow/workflow.go) for operations with persistent stages,
external approval/wait, retries, restart recovery or coordinated effects. Design the state graph,
authorization, atomic Commit writes, replay safety and recovery first. Follow the full
[workflow recipe](docs/conventions/workflow.md) for a definition and callbacks.

```go
import "github.com/tung-dnt/lean-go/pkg/workflow"

func definePublication[E, Tx any](prepare func(context.Context, E, any) error,
    publish func(context.Context, Tx, E) error,
) workflow.Definition[E, Tx] {
    return workflow.Definition[E, Tx]{
        Name: "publication",
        Failed: "failed",
        Finals: []workflow.State{"published", "failed"},
        Transitions: []workflow.Transition[E, Tx]{
            {From: "", To: "queued"},
            {From: "queued", To: "published", CountsAttempt: true, Do: prepare, Commit: publish,
                Retry: workflow.RetryPolicy{MaxAttempts: 3, Backoff: time.Second}},
            {From: "queued", To: "failed", When: workflow.Never[E]},
        },
    }
}
```

This generic definition fragment also needs `context` and `time` imports. `prepare` must be
replay-safe; `publish` must use the supplied transaction. Construct the orchestrator with a real
`Entity` store that enforces the atomic persistence contract; the runner does not supply a database.

**Don't** hand-roll a second state switch/retry orchestrator for these operations. Explain the
concrete limitation before bypassing the runner. Pure helper chains, read-only queries and one
atomic mutation remain ordinary code.

**Do** use [`pkg/worker`](pkg/worker/worker.go) to schedule process-owned callbacks:

```go
w, err := worker.New(worker.Config{
    Work: drainDueRows, // func(context.Context) time.Time; zero disarms the work deadline
    Tick: maintain,    // optional func(context.Context); independent of the work deadline
    Interval: time.Minute,
})
if err != nil {
    return fmt.Errorf("create worker: %w", err)
}
```

The process owner starts `Run(ctx)`, cancels it and joins before closing the database.
Call `Wake()` only after the durable row commits; startup scans recover work even without a wake.
Read [Background work](docs/conventions/background-work.md) for construction, lifecycle and cancellation.

**Don't** treat a wake hint as a queue, worker `Waiting` as business state, or the runtime as a
retry/SQL policy layer. Workflow and worker can be used independently; a request can advance a
workflow without a worker, and a worker can perform maintenance without a workflow.

## Add a domain

**Do** create `internal/<domain>` with concern/role files and external-package HTTP tests.
Make handlers with `HandleX(deps...) http.Handler`, taking only the dependencies they use.

1. Add migrations/queries and run `just sqlc` when persistence changes.
2. Register every route in `internal/server/routes.go` and add typed paths under `internal/server/paths`.
3. Add path test rows and the domain/import edges to `internal/architecture_test.go`.
4. Run `just check` and exercise the changed behavior.

**Don't** add routes inside domains or make the handler constructor read live dependencies;
route tests construct handlers with nil dependencies and exercise their routing separately.
Follow [handlers](docs/conventions/handlers-errors.md), [routes](docs/conventions/routes-paths.md),
[forms](docs/conventions/forms.md), [sessions](docs/conventions/session-auth.md) and
[data](docs/conventions/data.md) for concrete code.

## Tests and browser areas

**Do** test domain behavior through the real server and PostgreSQL. Use deterministic package tests
for the reusable workflow/worker contracts; domain persistence still needs real transaction and
restart tests. Docker supplies a disposable test database by default; `GO_DATABASE_TESTCONNECTION`
uses an existing database with a separate `test_$RAND` schema. See [testing](docs/conventions/testing.md).

**Do** bundle htmx and Alpine from npm through esbuild; no CDN. Keep browser areas explicit:
`app.ts`, `register.ts` and `globals.d.ts` may reference all areas; `note/` may reference `shell/`;
`shell/` imports no other area. Register Alpine components in `register.ts` and use `x-data` in
the views and pages (`charCount` in `internal/ui/pages/home_page.go`). Add new areas/edges to `webAreas`
in the architecture test and update this map together.

**Don't** bypass the import map with relative-path tricks or load unbundled scripts.
[Frontend](docs/conventions/frontend.md) covers the code; `just js` builds code-split assets and
`just css` builds the Tailwind/daisyUI classes from Go views.
