package middleware

import (
	"context"
	"crypto/rand"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/tung-dnt/lean-go/pkg/log"
)

// HeaderRequestID is the header that carries the id of a request, in and out.
const HeaderRequestID = "X-Request-Id"

// maxRequestIDLen is the longest id taken from a request. It is for a line of a log, and not for a client to
// fill one with.
const maxRequestIDLen = 64

// requestIDKey is the key of the id of the request in a context.
type requestIDKey struct{}

// RequestID gives every request an id and puts it in the X-Request-Id header of the response and in the context
// (RequestIDFrom). The id is the one the request came with, when it has a usable one: a proxy that already
// numbered it keeps the same id all the way. Otherwise it is a random one.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := r.Header.Get(HeaderRequestID)
		if !usableRequestID(id) {
			id = rand.Text()
		}

		w.Header().Set(HeaderRequestID, id)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDKey{}, id)))
	})
}

// usableRequestID reports whether a request id from a client is safe to log and to send back: short, and made
// of characters that cannot break a line of a log or a header.
func usableRequestID(id string) bool {
	if id == "" || len(id) > maxRequestIDLen {
		return false
	}
	for _, c := range id {
		switch {
		case c >= 'a' && c <= 'z', c >= 'A' && c <= 'Z', c >= '0' && c <= '9', c == '-', c == '_', c == '.':
		default:
			return false
		}
	}

	return true
}

// RequestIDFrom is the id that RequestID gave the request, or "" when it has none.
func RequestIDFrom(ctx context.Context) string {
	id, _ := ctx.Value(requestIDKey{}).(string)

	return id
}

// Logger puts a logger in the context of every request, for log.Ctx: base, with the id of the request when it has
// one. Placed after RequestID, everything that logs from there on, the access log, a handler, a service beneath
// it, says which request it was; placed alone, as in front of the routes of files, which have no id, it carries
// base itself, so that the log of the server that served the request is the one written to.
func Logger(base *slog.Logger) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			logger := base
			if id := RequestIDFrom(r.Context()); id != "" {
				logger = base.With("request_id", id)
			}

			next.ServeHTTP(w, r.WithContext(log.NewContext(r.Context(), logger)))
		})
	}
}

// LogRequests logs every request, once it is answered: what was asked for, who asked, what was answered, how
// much and how long it took. An answer of 500 and over is logged as an error and the rest as information.
func LogRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec := &recorder{ResponseWriter: w, status: http.StatusOK}
		start := time.Now()

		next.ServeHTTP(rec, r)

		bytesIn := r.Header.Get("Content-Length")
		if bytesIn == "" {
			bytesIn = "0"
		}

		level := slog.LevelInfo
		if rec.status >= http.StatusInternalServerError {
			level = slog.LevelError
		}
		log.Ctx(r.Context()).LogAttrs(r.Context(), level, r.Method+" "+r.URL.RequestURI(),
			slog.String("ip", RealIP(r)),
			slog.String("host", r.Host),
			slog.String("referer", r.Referer()),
			slog.Int("status", rec.status),
			slog.String("bytes_in", bytesIn),
			slog.String("bytes_out", strconv.FormatInt(rec.size, 10)),
			slog.String("latency", time.Since(start).String()),
		)
	})
}
