package middleware_test

import (
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/log"
	"github.com/tung-dnt/lean-go/pkg/middleware"
)

func TestRequestID(t *testing.T) {
	var seen string
	h := middleware.RequestID(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		seen = middleware.RequestIDFrom(r.Context())
	}))

	t.Run("gives a request an id, in the header and the context", func(t *testing.T) {
		rec := get(h, "/")

		id := rec.Header().Get(middleware.HeaderRequestID)
		assert.Len(t, id, 26)
		assert.Equal(t, id, seen)
	})

	t.Run("every request has its own", func(t *testing.T) {
		assert.NotEqual(t, get(h, "/").Header().Get(middleware.HeaderRequestID), get(h, "/").Header().Get(middleware.HeaderRequestID))
	})

	t.Run("keeps the id a proxy gave it", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set(middleware.HeaderRequestID, "from-the-proxy.123_A")

		rec := do(h, r)

		assert.Equal(t, "from-the-proxy.123_A", rec.Header().Get(middleware.HeaderRequestID))
		assert.Equal(t, "from-the-proxy.123_A", seen)
	})

	t.Run("does not take an id that a log or a header could not hold", func(t *testing.T) {
		for _, bad := range []string{strings.Repeat("a", 65), "has space", "new\nline", "quote\"", "<script>", "é"} {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			r.Header.Set(middleware.HeaderRequestID, bad)

			rec := do(h, r)

			assert.NotEqual(t, bad, rec.Header().Get(middleware.HeaderRequestID), bad)
			assert.Len(t, rec.Header().Get(middleware.HeaderRequestID), 26, bad)
		}
	})

	t.Run("a context without one has none", func(t *testing.T) {
		assert.Empty(t, middleware.RequestIDFrom(t.Context()))
	})
}

func TestLogger__EveryLineOfARequestHasItsId(t *testing.T) {
	c := newCapture()
	h := middleware.Chain(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		log.Ctx(r.Context()).Info("from the handler")
	}), middleware.RequestID, middleware.Logger(slog.New(c).With("app", "leango")))

	rec := get(h, "/")

	records := c.all()
	require.Len(t, records, 1)
	assert.Equal(t, rec.Header().Get(middleware.HeaderRequestID), records[0].attrs["request_id"])
	assert.Equal(t, "leango", records[0].attrs["app"], "what the base logger had")
}

func TestLogRequests(t *testing.T) {
	serve := func(status int, body string, mutate func(*http.Request)) record {
		c := newCapture()
		h := middleware.LogRequests(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(status)
			_, _ = w.Write([]byte(body))
		}))
		r := httptest.NewRequest(http.MethodGet, "http://test.localhost/abc?d=1&e=2", nil)
		r.Header.Set("Referer", "ref.com")
		r.RemoteAddr = "21.12.12.21:4000"
		if mutate != nil {
			mutate(r)
		}

		do(h, withLogger(t, c, r))

		records := c.all()
		require.Len(t, records, 1)

		return records[0]
	}

	t.Run("says who asked for what, and what came of it", func(t *testing.T) {
		got := serve(http.StatusOK, "hello", nil)

		assert.Equal(t, "GET /abc?d=1&e=2", got.msg)
		assert.Equal(t, slog.LevelInfo, got.level)
		assert.Equal(t, "21.12.12.21", got.attrs["ip"])
		assert.Equal(t, "test.localhost", got.attrs["host"])
		assert.Equal(t, "ref.com", got.attrs["referer"])
		assert.Equal(t, "200", got.attrs["status"])
		assert.Equal(t, "0", got.attrs["bytes_in"])
		assert.Equal(t, "5", got.attrs["bytes_out"])
		assert.NotEmpty(t, got.attrs["latency"])
	})

	t.Run("reads the size of the body that came in from its header", func(t *testing.T) {
		got := serve(http.StatusOK, "", func(r *http.Request) { r.Header.Set("Content-Length", "1234") })

		assert.Equal(t, "1234", got.attrs["bytes_in"])
	})

	t.Run("the status is the one that was written, and 200 when none was", func(t *testing.T) {
		assert.Equal(t, "404", serve(http.StatusNotFound, "", nil).attrs["status"])

		c := newCapture()
		do(middleware.LogRequests(okHandler), withLogger(t, c, httptest.NewRequest(http.MethodGet, "/", nil)))
		assert.Equal(t, "200", c.all()[0].attrs["status"])
	})

	t.Run("a 5xx is an error, and a 4xx is not", func(t *testing.T) {
		assert.Equal(t, slog.LevelError, serve(http.StatusInternalServerError, "", nil).level)
		assert.Equal(t, slog.LevelError, serve(http.StatusServiceUnavailable, "", nil).level)
		assert.Equal(t, slog.LevelInfo, serve(http.StatusNotFound, "", nil).level)
	})

	t.Run("the address is the real one", func(t *testing.T) {
		got := serve(http.StatusOK, "", func(r *http.Request) {
			r.RemoteAddr = "10.0.0.2:5000"
			r.Header.Set("X-Forwarded-For", "203.0.113.9")
		})

		assert.Equal(t, "203.0.113.9", got.attrs["ip"])
	})
}

func TestWritten(t *testing.T) {
	t.Run("sees through the writers that the middleware stack", func(t *testing.T) {
		var before, after bool
		h := middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			before = middleware.Written(w)
			_, _ = w.Write([]byte("body"))
			after = middleware.Written(w)
		}), middleware.LogRequests, middleware.Gzip)

		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Encoding", "gzip")
		do(h, r)

		assert.False(t, before)
		assert.True(t, after)
	})

	t.Run("is false for a writer that nobody keeps count for", func(t *testing.T) {
		assert.False(t, middleware.Written(httptest.NewRecorder()))
	})
}
