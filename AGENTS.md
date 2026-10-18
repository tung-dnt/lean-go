# Agent guide

A lean-go app: Go standard library `net/http`, PostgreSQL through sqlc, gomponents views, htmx and Alpine. This file is
always loaded, so it stays short. Read the rest only when the change needs it (the table below).

## Always

- Go standard library first; a dependency has to earn its place. HTTP is `net/http` with `http.ServeMux` (method + path patterns). No router framework, no ORM, no DI container, no reflection-driven wiring, no `init()` registration.
- Views are gomponents in Go; the browser side is htmx (hx-boost) and Alpine, bundled from npm by esbuild, styled with Tailwind + daisyUI.
- Errors are handled where they happen with an explicit `if err != nil` and wrapped with `%w`. No ignored errors (`_ = f()`), no `panic` for control flow, no empty error branches.
- No interface that exists only so a test can fake it. Tests run the real server against a real PostgreSQL.
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
| run work that cannot finish inside a request | [background-work](docs/conventions/background-work.md) |
| write or change a test | [testing](docs/conventions/testing.md) |
| touch `web/js` | [frontend](docs/conventions/frontend.md) |
| touch a migration, a query or `internal/external/db` | [data](docs/conventions/data.md) |
| want to skip, suppress or loosen a check, or change a threshold | [CONSTRAINTS.md](CONSTRAINTS.md) |
