# Routes, paths and guards

Read `internal/server/routes.go` before changing routing; `addRoutes` is the route registry. Paths are shared by handlers, templates and tests through `internal/server/paths`.

## Register and link routes

**Do:** add routes in `addRoutes` under the domain comment, use Go's method-aware `ServeMux` patterns, and add the path to `paths` plus `internal/server/paths_test.go` (including its wildcard table when it contains a wildcard). `GET` also serves `HEAD`.

```go
mux.Handle("GET /notes/{id}", note.HandleShow(d.queries))
```

Read a path parameter with `r.PathValue("id")`. Use `GET /{$}` for the home page: bare `GET /` matches the subtree and would take unmatched GETs away from the `"/"` not-found catch-all. Never register a page pattern ending in `/`; trailing-slash redirects interact with the removal middleware.

Use `paths.Note(id)` or `paths.Home` in links, form actions and redirects—never spell route strings at callsites. Route-path tests ensure links resolve to registered routes.

**Don't:** make a GET mutate data. Use POST for actions; successful posts should redirect with 303 so reload does not resubmit.

## Middleware and dependency construction

Global middleware is assembled in `NewServer`, outermost first, with placement rationale next to the chain. Files and `/healthz` are bare routes and skip that chain. Route-specific wrappers are composed in `routes.go`: `middleware.RequireAuth(fail)`, `RequireNoAuth`, `CacheControl`, `GzipFiles`, and `LargeBody` for routes that accept larger bodies. `LargeBody(limit, timeout, layouts.Error)` caps body reads and adjusts read/write and request deadlines for that route.

Every path at or below `/admin` is protected by the admin check in `NewServer`, including routes added later. Do not weaken that boundary by relying on a per-route wrapper. For dependencies such as external clients, construct them in `Run`, pass them through the concrete server dependencies, and use only what each handler needs.

A handler maker keeps what it is given and does not call dependencies during registration. Route tests may build handlers with nil dependencies; dependency access belongs in request handling, not maker construction.
