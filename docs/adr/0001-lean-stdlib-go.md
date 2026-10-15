# 0001. Lean stdlib Go: net/http ServeMux, flat domains, E2E tests

Status: accepted (inherited from the template `tung-dnt/lean-go`, which extracted it from a production app)

- **Context:** Starter kits for Go web apps (Pagoda and the like) bring a router framework, a service container with `init()` registration, gorilla sessions, go-playground/validator, CSRF tokens, an ORM, and one folder per role (`domain/ services/ views/ adapters/`) inside each module. That is a lot of structure and library surface for a small app, and it is hard to read a request from route to query.
- **Decision:**
  - **HTTP:** `net/http` with `http.ServeMux` (Go 1.22+ method and path patterns). `cmd/web/main.go` only calls `server.Run`; `server.NewServer` wraps the mux in middleware (`func(http.Handler) http.Handler`); `internal/server/routes.go` lists every route; handlers are `HandleX(deps...) http.Handler` makers that take only what they use, passing concrete types. No router framework, no DI container, no `init()` registration. Typed path functions in `internal/server/paths` replace named routes.
  - **Data:** PostgreSQL through pgx, with sqlc generating typed Go from hand-written SQL and golang-migrate migrations embedded in the binary. No ORM.
  - **Security and forms:** CSRF through `http.CrossOriginProtection` (no tokens). Sessions are one AES-256-GCM sealed cookie (current key, then the previous one). Validation is a `Valid(ctx) map[string]string` method on each form struct. Everything under `/admin` is held to the admin check in front of the router.
  - **Layout:** each domain is one flat package split by concern (`<domain>.go`, `views.go`, `state.go`). Shared code is in `pkg`, third-party adapters and the database in `internal/external`, the page shell in `internal/ui`. `internal/architecture_test.go` enforces the edges between them, the banned libraries, and the same kind of edges between the areas of `web/js`.
  - **Tests:** end to end. A test starts the real server with `server.Run` (config through `getenv`) against a real PostgreSQL (testcontainers) and drives it over HTTP; faults are injected at one named sqlc query. Pure-function tests and the SQL tests of `internal/external/db` stay. No interface exists only so a test can fake it.
  - **Logging:** no global logger; `Run` builds one and the request middleware carries it in the context (`log.Ctx`), so several servers in one test process do not share it.
- **Why:** Less code to read and fewer dependencies to patch; behaviour is tested the way a visitor meets it.
- **Revisit if:** a route table or middleware chain outgrows one `routes.go`, or a second binary needs a domain, which would call for splitting the server package.
