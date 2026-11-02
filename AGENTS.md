# Agent guide

A lean-go app: Go standard library `net/http`, PostgreSQL through sqlc, gomponents views, htmx and Alpine. This file is
always loaded, so it stays short. Read the rest only when the change needs it (the table below).

## Always

- Go standard library first; a dependency has to earn its place. HTTP is `net/http` with `http.ServeMux` (method + path patterns). No router framework, no ORM, no DI container, no reflection-driven wiring, no `init()` registration.
- Before implementing new logic, inspect `pkg/` for a reusable function or mechanism. Read its contract and callers, and reuse a suitable implementation instead of duplicating it. If nothing fits, explain the concrete gap; keep domain-specific behavior in its owning domain.
- Views are gomponents in Go; the browser side is htmx (hx-boost) and Alpine, bundled from npm by esbuild, styled with Tailwind + daisyUI.
- Errors are handled where they happen with an explicit `if err != nil` and wrapped with `%w`. No ignored errors (`_ = f()`), no `panic` for control flow, no empty error branches.
- No interface that exists only so a test can fake it. Tests run the real server against a real PostgreSQL.
- Keep each domain flat and name every non-test file directly in `internal/<domain>` `<pkg>.go` or `<concern>_<role>.go`, role from the closed list in [LEANGO.md](LEANGO.md) ("Flat domains"): `http`, `form`, `store`, `worker`, `effects`, `test_support`, `workflow_definition`, `workflow_<step>`. One file handles an incoming request: controller plus all its processing logic; there is no `_api`/`_query`/`_policy` file. `_store` is only for persistence used by two or more roles. `<pkg>.go` holds the package doc, shared types/consts/sentinels and methods on them, no other function. Place each function by its callers; no role or home fits: stop and ask the user, never invent a suffix. R1: the `_http.go` file is the controller and calls domain functions directly; no `_service`/`_controller`/forwarding layer. R2: a domain imports no other domain; code two or more domains share goes in `internal/shared/<concern>` (one subpackage per concern, no flat `common`; not held to the role list). Read the owning definition and all its callbacks before changing a workflow; do not hide steps in workflow subpackages.
- Every full HTML page (a URL a browser navigates to) is a render function in the flat package `internal/ui/pages`, one file per page named by its URL path: segments joined by `_`, `{x}` written as lower-case `x`, then `_page.go` (`/` is `home_page.go`, `/notes/{id}` is `notes_id_page.go`). Only `_page.go` files live there (tests in `internal/ui/pages/test/`). A page is a template: plain function parameters (built-ins, `internal/ui/components`, `internal/shared/*`, `pkg/*` types), never a domain or `internal/external` import. The domain's `<concern>_http.go` handler queries, maps and calls the page, and chooses the layout; fragments, POST/action answers, SSE frames and JSON live in the handler's own `<concern>_http.go` (there is no `_views` file), a page rendered in the site layout goes through `layouts.Page`, and markup that a page and a fragment both draw goes to `internal/ui/components`. See [LEANGO.md](LEANGO.md) ("Full pages").
- Prefer `pkg/workflow` for multi-step domain operations with persistent stages, approval/wait states, retries, restart recovery or coordinated effects. Before coding, read [Workflows](docs/conventions/workflow.md), identify or extend the owning definition, and specify edges, atomic Commit writes, idempotency and recovery. Do not hand-roll status switches or retry orchestration alongside the runner. Explain a concrete limitation before bypassing it. Pure helper chains, read-only queries and one atomic mutation do not need a workflow.
- Use `pkg/worker` for process-owned background execution, passing callbacks from the owning domain. Keep lifecycle, wake signals and timers in the runtime; durable job state, queries and retry policy stay in the domain. Read [Background work](docs/conventions/background-work.md); do not copy a scheduling loop.
- Test first, end to end. A task is done when `just check` (`sqlc-verify sqlc-vet lint vuln types secrets deps dupes test build`) is green.

## Floor

- No skipped or deleted tests, and no assertions removed from tests that stay, without a reason in the commit message. A test that pinned markup is rewritten to pin behaviour (roles, `data-*`, ids, text), never dropped.
- No unimplemented stubs: `panic("TODO")`, `// TODO: implement`, functions that return zero values where logic belongs, empty error handling (`_ = err`, `if err != nil {}`).
- No new suppressions: `//nolint`, `// @ts-ignore`, `// @ts-expect-error`, `eslint-disable`, `gitleaks:allow`.
- No secrets in source. `config/config.yaml` holds only local values; a live environment overrides every secret with a `GO_*` variable.
- Go: `go vet ./...` clean, `golangci-lint run` (`.golangci.yml`) reports nothing, and `internal/architecture_test.go` passes (module and web edges).
- Neither this Floor nor [CONSTRAINTS.md](CONSTRAINTS.md) is weakened in the same PR as the change that was failing.

## Read before you

| Change | Read |
|---|---|
| add a domain, or an import between packages or `web/js` areas | [LEANGO.md](LEANGO.md) (Architecture, Adding a domain) |
| write or change a handler, its errors or its rendering | [handlers-errors](docs/conventions/handlers-errors.md) |
| add a route, a path, a guard or per-route middleware | [routes-paths](docs/conventions/routes-paths.md) |
| read a form, a file upload or a large body | [forms](docs/conventions/forms.md) |
| flash, redirect, log in or out, ask who is logged in | [session-auth](docs/conventions/session-auth.md) |
| implement multi-step logic, states, retries, approval or recovery | [Workflows](docs/conventions/workflow.md), then the owning `<concern>_workflow_definition.go` and its callbacks |
| run work that cannot finish inside a request | [Workflows](docs/conventions/workflow.md) and [background-work](docs/conventions/background-work.md) |
| write or change a test | [testing](docs/conventions/testing.md) |
| touch `web/js` | [frontend](docs/conventions/frontend.md) |
| touch a migration, a query or `internal/external/db` | [data](docs/conventions/data.md) |
| want to skip, suppress or loosen a check, or change a threshold | [CONSTRAINTS.md](CONSTRAINTS.md) |
