package middleware

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/log"
)

// timerKey is the key of the timer of a request's deadline in its context.
type timerKey struct{}

// Timeout gives every request d to be handled. When the time is up the context of the request is cancelled, with
// httperr.ErrTimeout as its cause, and whatever the handler is waiting for that takes the context (a query, a
// call to the storage) returns at once; the handler answers with the error, and the error page says 503 for a
// context that was cancelled for this cause. A d that is not positive is no limit.
//
// It does not run the handler in a goroutine of its own, nor does it stand between the handler and the
// response, as http.TimeoutHandler does. That is how it is free of data races: the handler, the session and
// everything else of the request stay on the one goroutine that the server gave them, and the price is that a
// handler that waits for something that does not take the context is not interrupted. The server's own
// ReadTimeout and WriteTimeout still cut the connection off, so a request cannot be held open by a client, and
// all the work of this application takes the context.
//
// It goes before anything that waits, and before LargeBody, which lifts it for the route that is allowed more.
func Timeout(d time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		if d <= 0 {
			return next
		}

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx, cancel := context.WithCancelCause(r.Context())
			defer cancel(nil)

			// A timer, and not context.WithTimeout, so that the deadline can be moved (ExtendTimeout).
			timer := time.AfterFunc(d, func() { cancel(httperr.ErrTimeout) })
			defer timer.Stop()

			next.ServeHTTP(w, r.WithContext(context.WithValue(ctx, timerKey{}, timer)))
		})
	}
}

// ExtendTimeout moves the deadline of the request of ctx to d from now, in place of what is left of it: for a
// route that is allowed to take longer than the others. It reports whether there was a deadline to move, which
// there is not for a request that did not go through Timeout, nor for one whose time is already up.
func ExtendTimeout(ctx context.Context, d time.Duration) bool {
	timer, ok := ctx.Value(timerKey{}).(*time.Timer)
	if !ok || ctx.Err() != nil {
		return false
	}
	timer.Reset(d)

	return true
}

// LargeBody lets the route it is put on take a body of up to limit bytes, in up to timeout, which is what a
// route that takes an upload needs and no other route is given. It does for that route what the application
// does for none of the others:
//
//   - the body is capped at limit bytes with http.MaxBytesReader: a body that announces more is refused before
//     a byte of it is read, and one that does not and goes over is cut off, with an *http.MaxBytesError, which
//     the form decoder passes on and the error page answers with a 413;
//   - the connection's read and write deadlines are moved to timeout from now, in place of the server's
//     ReadTimeout and WriteTimeout (seconds, which a file of tens of megabytes over an ordinary uplink does not
//     fit in), and the request's own deadline (Timeout) is moved with them. Raising the server's limits would
//     give every route the same long window, and a client that drips a request in slowly would hold the
//     connection of any route that long.
//
// A timeout that is not positive moves no deadline, and the route keeps the ones everything has. fail answers
// the request whose body is refused up front.
//
// Put it on the one route, in routes.go: mux.Handle("POST "+paths.Upload, middleware.LargeBody(limit, d, fail)(h)).
func LargeBody(limit int64, timeout time.Duration, fail ErrorFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > limit {
				fail(w, r, &http.MaxBytesError{Limit: limit})

				return
			}

			if timeout > 0 {
				extendDeadlines(w, r, timeout)
				ExtendTimeout(r.Context(), timeout)
			}
			r.Body = http.MaxBytesReader(w, r.Body, limit)

			// The form decoder reads 1 MiB of a body unless the route says more: this route does.
			next.ServeHTTP(w, r.WithContext(form.WithMaxBytes(r.Context(), limit)))
		})
	}
}

// extendDeadlines sets the read and write deadlines of the connection of w, d from now.
func extendDeadlines(w http.ResponseWriter, r *http.Request, d time.Duration) {
	rc := http.NewResponseController(w)
	deadline := time.Now().Add(d)

	extend := func(which string, set func(time.Time) error) {
		// A writer with no connection behind it, such as the recorder of a test that calls the handler directly,
		// has no deadline to move and needs none. Any other error means the connection is already unusable, which
		// the request is going to find out for itself.
		if err := set(deadline); err != nil && !errors.Is(err, http.ErrNotSupported) {
			log.Ctx(r.Context()).Warn("failed to extend a request deadline", "deadline", which, "error", err)
		}
	}

	extend("read", rc.SetReadDeadline)
	extend("write", rc.SetWriteDeadline)
}
