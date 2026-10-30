# Forms and large request bodies

`note.noteForm` and `note.HandleCreate` are the worked example. Form input is decoded and validated by `pkg/form`; request-size and timeout limits are applied at the route.

## Decode and validate

**Do:** define a form struct with `form:"name"` tags, embed `form.Submission`, and implement `Valid(ctx) map[string]string`. Keys are Go field names; values are field messages. Use `form.Decode` for url-encoded or multipart bodies, then check `IsValid` before writing. Add handler-only validation with `SetFieldError`.

```go
var in noteForm
if err := form.Decode(r, &in); err != nil {
    layouts.Error(w, r, err)
    return
}
if !in.IsValid() {
    renderListOrFail(w, r, q, &in)
    return
}
// Continue with the validated form.
```

`Decode` does not read query parameters. It reports unreadable/unsupported bodies (400/415) and size overflow (`*http.MaxBytesError`, 413); invalid field values are represented on the form, not returned as a decode error. Default decode limit is 1 MiB. Multipart files are read with `r.FormFile` after Decode; the server's `form.Cleanup` middleware removes temporary files. Use `form.MsgRequired`, `form.IsEmail` and `form.MsgEmail` for the shared rules—there are no `validate:` tags. Repeat important limits in database constraints so they cannot be bypassed by non-HTTP writes.

**Don't:** read posted fields from `r.URL.Query()`, silently ignore a decode error, or accept a form as valid just because decoding succeeded.

## Routes that accept larger bodies

Apply `middleware.LargeBody(limit, timeout, fail)` to the specific route that needs it. It rejects a known oversized `Content-Length` before reading and caps streaming bodies with `http.MaxBytesReader`; it also raises the form decoder's body allowance to that limit. Both overflow paths produce `*http.MaxBytesError` and normally render the generic 413 message.

If the domain needs its own 413 message, wrap the error in an `ErrorFunc` passed to `LargeBody` and map the decoder's error in the handler too:

```go
if err := form.Decode(r, &in); err != nil {
    var tooLarge *http.MaxBytesError
    if errors.As(err, &tooLarge) {
        err = httperr.New(http.StatusRequestEntityTooLarge, "This request is too large.")
    }
    layouts.Error(w, r, fmt.Errorf("decode submitted form: %w", err))
    return
}
```

Use the route-level body timeout and limit deliberately; do not raise the global limit for one large upload route.
