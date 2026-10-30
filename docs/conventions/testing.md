# Tests

Test behavior end to end first: exercise the real server, handlers and PostgreSQL with the request a browser makes, then implement the change. See `internal/note/note_test.go` for the worked example.

## End-to-end tests

Tests for a domain live in its external package (for example `package note_test`) and use `internal/server/servertest`. The server imports every domain, so an in-package test that imports the server would create an import cycle. One `TestMain` starts the shared real server:

```go
var srv *servertest.Server

func TestMain(m *testing.M) {
    os.Exit(servertest.Main(m, func(s *servertest.Server) { srv = s }))
}
```

Drive it as a browser with `servertest.NewVisitor(t)` (cookie jar; redirects are not followed), `srv.Visit`, `srv.PostForm`, `srv.LogIn`/`LogInAs`, and `servertest.Fragment` for HTML. Use `srv.NewRequest` plus `srv.Do` for custom headers. `srv.Queries` and `srv.DB` prepare or inspect rows. The package's tests share a database, so create uniquely named fixtures and clean up where appropriate.

Use `servertest.Start(t, servertest.WithEnv(...))` for an isolated server/schema when a test must alter configuration, count all rows, or break the database. `srv.FailQuery(t, table, query)` makes one real sqlc-named query fail with `"boom"`; `srv.HideRows(t, table, query)` makes it return no rows; `srv.Heal(t)` removes the fault and `srv.Log()` exposes server logs. Prefer these targeted faults over replacing the database or mocking handlers.

Tests need Docker for the process's first `postgres:17-alpine` testcontainer (`pkg/testdb`); each process uses a random schema. Set `GO_DATABASE_TESTCONNECTION` to use an existing PostgreSQL test database instead. Cross-origin protection refuses cross-site posts; test clients without `Origin` and `Sec-Fetch-Site` are allowed, so a plain `PostForm` does not need a CSRF-token fetch.

## Unit tests and database behavior

Keep unit tests for pure functions and SQL-specific behavior in `internal/external/db`. The generic contract tests for `pkg/workflow` and `pkg/worker` are also an exception: package-level tests should exercise graph validation, Start/Run/Advance behavior, Commit rollback, conflict/replay semantics, OnError result/error outcomes, the initial Work callback, wake coalescing, serial callbacks, zero/deadline replacement, independent Tick timing, cancellation/join, and invalid configuration. End-to-end domain tests should prove that the real durable startup scan recovers work. A test-only in-memory `Entity` may model the workflow store contract for runner mechanics, but it does not prove that a domain's real persistence is atomic or durable.

A query with a rule of its own—ordering, locking, or a limit—needs a test proving that rule. Domain persistence, transaction, route, session and authorization behavior should be exercised through the real server and database rather than test-only fake implementations.

**Don't:** make tests pass by weakening assertions or replacing real persistence with a fake that only echoes inputs. Assert observable response status, redirects, rendered markup, persisted state and relevant logs; use isolated server instances when shared state would make a test ambiguous.
