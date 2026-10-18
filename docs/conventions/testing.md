# Tests

Test first: write the end-to-end test through the real server (real handlers, real PostgreSQL), watch it fail for the right reason, then write the code.

End to end: `servertest.Main` starts the real server (`server.Run`) against a real PostgreSQL, and a test drives it
with HTTP requests as a browser would (cookie jar, no redirect following, goquery for the markup). A test that has to
break the database, or count every row, starts a server of its own with `servertest.Start`, and
`FailQuery(t, table, "ListNotes")` makes one sqlc-named query fail while every other query is the real one. Only pure
functions and the SQL of `internal/external/db` have unit tests. A test package is the external one of its domain
(`package note_test`).

Tests need Docker and nothing else: the first test of each process starts a throwaway `postgres:17-alpine`
(`pkg/testdb`, testcontainers), and each process isolates itself in a randomly-named schema. Set
`GO_DATABASE_TESTCONNECTION` to use a database you already have (CI does).

- **End-to-end tests.** Tests are in the external package of the domain (`package note_test`), never in the package itself (the server imports every domain, so a test that imports the server from inside would be an import cycle); pure-function tests may stay internal. One `TestMain` starts the real server:

  ```go
  var srv *servertest.Server

  func TestMain(m *testing.M) { os.Exit(servertest.Main(m, func(s *servertest.Server) { srv = s })) }
  ```

  and a test drives it as a browser does: `visitor := servertest.NewVisitor(t)` (cookie jar, no redirect following), `srv.Visit(t, visitor, http.MethodGet, paths.Login)`, `srv.PostForm`, `srv.LogIn`, `srv.LogInAs(t, admin)`, `srv.NewAccount`, `srv.NewRequest`+`srv.Do` for headers, `servertest.Fragment` (goquery). `srv.Queries`/`srv.DB` make rows. `servertest.Start(t, servertest.WithEnv(...))` is a server of its own, with a schema of its own, for a test that must break the database, count every row, or change the configuration. The database is broken at one query, the real one, with `srv.FailQuery(t, table, query)` (the query that sqlc named so fails with "boom" when it reads the table) or `srv.HideRows(t, table, query)` (it finds nothing there, as when a row went between two queries of a request), `srv.Heal(t)` takes the fault away, and `srv.Log()` is what the server logged (take its length before a request and read what comes after). The tests of a package share one database: make what you look for with a name that nothing else has. There is no CSRF token to fetch: a cross-site post is refused by `http.CrossOriginProtection` (a post without `Origin` and `Sec-Fetch-Site`, as a test client sends, is let through). `internal/note/note_test.go` shows each of these.
