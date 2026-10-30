# Session, flash, redirects and the user

Sessions are handled by `pkg/session`; authentication handlers are in `internal/auth`; user loading and guards are server middleware. The current template has one admin account created by `cmd/admin`, not public registration, password reset, or email verification.

## Use the session and redirect helpers

**Do:** access the current session from `session.From(r.Context())`, mutate it with `SetUserID` or `Clear`, and let session middleware write the cookie. Get the authenticated user with `middleware.User(r.Context())`; views use `page.IsAuth` or `page.IsAdmin`. `middleware.LoadUser` loads the user for every page, so domains do not import `internal/auth` to identify the requester.

```go
sess, ok := session.From(r.Context())
if !ok {
    layouts.Error(w, r, errors.New("request has no session"))
    return
}
sess.Clear()
sess.SetUserID(user.ID)
msg.Success(r.Context(), "Welcome back.")
redirect.To(w, r, paths.Home, http.StatusSeeOther)
```

For a successful form POST, use `http.StatusSeeOther` (303): a browser follows with GET instead of posting credentials or data again. `redirect.To` sends boosted htmx requests an `HX-Redirect`. Use `msg.Success`, `Info`, `Warning`, or `Error` to queue a flash message in the request session; the next rendered page consumes it. Do not write the session cookie yourself or mutate the session after response headers have been sent.

## Cookie and request security

The session is a stateless AES-256-GCM sealed cookie containing the session values and expiration. The cookie is HttpOnly and SameSite=Strict, and is Secure when TLS is enabled, the request is TLS, or the proxy says `X-Forwarded-Proto: https`. It is written only when changed. A cookie that is altered, expired, or sealed by a removed key becomes an empty session and is removed; it is not an application error. Key rotation can configure a previous key, whose accepted cookie is rewritten under the current key. Keep the encryption key secret and non-empty.

A copied cookie remains valid until expiry even after logout; logout clears the browser's current cookie, not previously copied values. The session cookie is not a server-side revocation list. `CrossOrigin` protection runs before session middleware and refuses cross-origin posts. Do not bypass it or rely on SameSite alone as the request-integrity check. Protected route guards are assembled in `internal/server/routes.go`; `/admin` is covered centrally by `NewServer` and requires the admin guard.

**Don't:** trust a user ID supplied by form/query/header input, import `internal/auth` from a domain, expose whether an email exists during login, or assume that a redirect has persisted a session change independently of the middleware.
