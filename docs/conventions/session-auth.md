# Session, flash, redirect and the user

How a handler says what happened, sends the browser on, and knows who is logged in. `auth.HandleLoginSubmit` is the worked example.

- **Session, flash, redirect, user.** `msg.Success(r.Context(), "...")` (also `Info`, `Warning`, `Error`) and then `redirect.To(w, r, paths.Home, http.StatusSeeOther)`, which answers a boosted htmx request with `HX-Redirect`. A post that worked answers with a 303, so that a reload does not post it again. Who is logged in: `middleware.User(r.Context())`, or `page.IsAuth`/`page.IsAdmin` in a view. Logging in and out is the session: `sess, ok := session.From(r.Context())`, `sess.Clear()`, `sess.SetUserID(id)` (`auth.HandleLoginSubmit`). The session is one AES-256-GCM sealed cookie, written by the middleware only when it changed; a cookie that does not open is an empty session. Domains never import `internal/auth`: the server loads the user (`middleware.LoadUser`).
