package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"runtime/debug"

	"github.com/tung-dnt/lean-go/pkg/log"
)

// Recover turns a panic in the handlers below it into a 500 and a line in the log with the stack, and not into a
// dropped connection. When the response had been started the page can no longer be an error page, and the
// connection is ended, so that the client does not take half a page for a whole one.
func Recover(fail ErrorFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				recovered := recover()
				if recovered == nil {
					return
				}

				// By convention http.ErrAbortHandler is a handler that wants the response aborted, and nobody
				// wants a stack for it.
				if err, ok := recovered.(error); ok && errors.Is(err, http.ErrAbortHandler) {
					panic(recovered)
				}

				log.Ctx(r.Context()).Error("panic while handling the request",
					"panic", fmt.Sprint(recovered),
					"stack", string(debug.Stack()),
				)

				if Written(w) {
					panic(http.ErrAbortHandler)
				}
				fail(w, r, fmt.Errorf("panic: %v", recovered))
			}()

			next.ServeHTTP(w, r)
		})
	}
}
