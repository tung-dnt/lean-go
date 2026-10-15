package middleware

import (
	"errors"
	"net/http"

	"github.com/tung-dnt/lean-go/pkg/log"
)

// recorder remembers what the handler below it wrote: the status, the size of the body, and whether the response
// has been started, which is when it can no longer be turned into an error page.
type recorder struct {
	http.ResponseWriter
	status int
	size   int64
	wrote  bool
}

// WriteHeader implements http.ResponseWriter.
func (rw *recorder) WriteHeader(code int) {
	// A 1xx is informational and the final status is still to come.
	if !rw.wrote && (code >= 200 || code == http.StatusSwitchingProtocols) {
		rw.status = code
		rw.wrote = true
	}
	rw.ResponseWriter.WriteHeader(code)
}

// Write implements http.ResponseWriter.
func (rw *recorder) Write(b []byte) (int, error) {
	if !rw.wrote {
		rw.status = http.StatusOK
		rw.wrote = true
	}
	n, err := rw.ResponseWriter.Write(b)
	rw.size += int64(n)

	return n, err
}

// Flush implements http.Flusher.
func (rw *recorder) Flush() {
	if !rw.wrote {
		rw.status = http.StatusOK
		rw.wrote = true
	}
	if err := http.NewResponseController(rw.ResponseWriter).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		log.Default().Debug("flush", "error", err)
	}
}

// Unwrap lets http.ResponseController reach the writer beneath, for its deadlines and for hijacking.
func (rw *recorder) Unwrap() http.ResponseWriter {
	return rw.ResponseWriter
}

// Written reports whether the response has been started.
func (rw *recorder) Written() bool {
	return rw.wrote
}

// Written reports whether the response that w writes to has been started, that is, whether the status line has
// gone out and an error page can no longer be the answer. It looks through the writers that the middleware put
// on top of each other, so it is true for any of them as long as the one that keeps count is in the chain; it
// is false for a writer that nobody keeps count for.
func Written(w http.ResponseWriter) bool {
	for {
		if counter, ok := w.(interface{ Written() bool }); ok {
			return counter.Written()
		}

		inner, ok := w.(interface{ Unwrap() http.ResponseWriter })
		if !ok {
			return false
		}
		w = inner.Unwrap()
	}
}
