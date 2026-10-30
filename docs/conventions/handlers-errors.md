# Handlers, errors and rendering

Use `internal/note` as the worked example; `internal/auth` shows the second domain pattern. Read `internal/server/server.go` before changing middleware behavior.

## Handler shape

**Do:** expose a handler maker `HandleX(deps...) http.Handler` that captures only the dependencies it uses and prepares nothing that needs request data. A handler without dependencies takes none; a database-backed handler receives `*db.Queries`. Keep the domain flat and give each handler/view concern its own file. For each request, `r` is `*http.Request`; create the view request with `page := ui.NewRequest(r)`.

Pass `r.Context()` to every query and downstream call. It carries the request deadline configured by `app.timeout`; timeout cancellation uses `httperr.ErrTimeout`. Log with `log.Ctx(r.Context())` to retain the request ID, and obtain the client address with `middleware.RealIP(r)` rather than trusting a raw forwarding header.

## Errors

Every failure goes through the shared error renderer and then returns:

```go
if err != nil {
    layouts.Error(w, r, fmt.Errorf("load note: %w", err))
    return
}
```

Wrap errors with `%w` and the failed operation. Use `httperr.New(status, message)` for a deliberate status. `layouts.Error` logs 5xx as errors and 4xx as warnings; it maps `*http.MaxBytesError` to 413, request timeout to 503, and a disconnected client to no response. Other unexpected errors become 500. Only the 413 message is shown to the user; never expose internal error text. Do not call `http.Error` or set an error status directly.

## Render pages

Use `ui.Render(w, page, layouts.Primary, node)` for 200 and `ui.RenderStatus(w, status, page, layout, node)` for another successful/rendered status. Never call `WriteHeader` before rendering: flash messages are consumed while the page is built, and their clearing cookie must be written with the response headers. If drawing a view fails, route the error through `layouts.Error` (see `note.draw`). View files import gomponents as `g`/`h` and qualify the components package; do not use dot imports.

**Don't:** write partial page output before handling an error or bypass the renderer with ad-hoc status/body responses.
