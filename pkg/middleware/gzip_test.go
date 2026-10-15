package middleware_test

import (
	"bytes"
	"compress/gzip"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/middleware"
)

// page is a handler that writes some HTML, with no content type, as the page handlers do.
var page = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("<!doctype html><html><body>" + strings.Repeat("hello ", 100) + "</body></html>"))
})

func acceptingGzip(method, target string) *http.Request {
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Accept-Encoding", "gzip, deflate, br")

	return r
}

func unzip(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()

	zr, err := gzip.NewReader(bytes.NewReader(rec.Body.Bytes()))
	require.NoError(t, err)
	b, err := io.ReadAll(zr)
	require.NoError(t, err)
	require.NoError(t, zr.Close())

	return string(b)
}

func TestGzip(t *testing.T) {
	h := middleware.Gzip(page)
	plain := get(page, "/").Body.String()

	t.Run("compresses for a client that takes gzip", func(t *testing.T) {
		rec := do(h, acceptingGzip(http.MethodGet, "/"))

		assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
		assert.Contains(t, rec.Header().Get("Vary"), "Accept-Encoding")
		assert.Equal(t, plain, unzip(t, rec))
		assert.Less(t, rec.Body.Len(), len(plain))
	})

	t.Run("says what it sent, since the bytes are no longer something to sniff", func(t *testing.T) {
		rec := do(h, acceptingGzip(http.MethodGet, "/"))

		assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	})

	t.Run("leaves the type that the handler chose", func(t *testing.T) {
		typed := middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"a":1}`))
		}))

		assert.Equal(t, "application/json", do(typed, acceptingGzip(http.MethodGet, "/")).Header().Get("Content-Type"))
	})

	t.Run("does not compress for a client that does not take it", func(t *testing.T) {
		for _, accept := range []string{"", "deflate", "br", "gzip;q=0", "identity"} {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
			if accept != "" {
				r.Header.Set("Accept-Encoding", accept)
			}

			rec := do(h, r)

			assert.Empty(t, rec.Header().Get("Content-Encoding"), accept)
			assert.Equal(t, plain, rec.Body.String(), accept)
			assert.Contains(t, rec.Header().Get("Vary"), "Accept-Encoding", "the cache has to know it depends on it: %s", accept)
		}
	})

	t.Run("a quality is not a refusal unless it is zero", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodGet, "/", nil)
		r.Header.Set("Accept-Encoding", "gzip;q=0.5")

		assert.Equal(t, "gzip", do(h, r).Header().Get("Content-Encoding"))
	})

	t.Run("sets the content length of nothing it sends", func(t *testing.T) {
		sized := middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Length", "5")
			_, _ = w.Write([]byte("hello"))
		}))

		rec := do(sized, acceptingGzip(http.MethodGet, "/"))

		assert.Empty(t, rec.Header().Get("Content-Length"), "it is the length of the plain body")
		assert.Equal(t, "hello", unzip(t, rec))
	})

	t.Run("a status with a body is compressed, and its status kept", func(t *testing.T) {
		notFound := middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = w.Write([]byte("<p>nothing here</p>"))
		}))

		rec := do(notFound, acceptingGzip(http.MethodGet, "/"))

		assert.Equal(t, http.StatusNotFound, rec.Code)
		assert.Equal(t, "<p>nothing here</p>", unzip(t, rec))
	})

	t.Run("a response with no body is not an empty gzip stream", func(t *testing.T) {
		for name, status := range map[string]int{"a redirect": http.StatusSeeOther, "a not found": http.StatusNotFound, "not modified": http.StatusNotModified, "no content": http.StatusNoContent} {
			bodiless := middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Location", "/elsewhere")
				w.WriteHeader(status)
			}))

			rec := do(bodiless, acceptingGzip(http.MethodGet, "/"))

			assert.Equal(t, status, rec.Code, name)
			assert.Empty(t, rec.Header().Get("Content-Encoding"), name)
			assert.Empty(t, rec.Body.String(), name)
		}
	})

	t.Run("a handler that never writes is a 200 with no body", func(t *testing.T) {
		rec := do(middleware.Gzip(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {})), acceptingGzip(http.MethodGet, "/"))

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Empty(t, rec.Header().Get("Content-Encoding"))
	})

	t.Run("leaves what is already encoded", func(t *testing.T) {
		encoded := middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Encoding", "br")
			_, _ = w.Write([]byte("brotli bytes"))
		}))

		rec := do(encoded, acceptingGzip(http.MethodGet, "/"))

		assert.Equal(t, "br", rec.Header().Get("Content-Encoding"))
		assert.Equal(t, "brotli bytes", rec.Body.String())
	})

	t.Run("leaves a range, a partial answer and a HEAD", func(t *testing.T) {
		ranged := acceptingGzip(http.MethodGet, "/")
		ranged.Header.Set("Range", "bytes=0-9")
		assert.Empty(t, do(h, ranged).Header().Get("Content-Encoding"))

		partial := middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusPartialContent)
			_, _ = w.Write([]byte("part"))
		}))
		rec := do(partial, acceptingGzip(http.MethodGet, "/"))
		assert.Empty(t, rec.Header().Get("Content-Encoding"))
		assert.Equal(t, "part", rec.Body.String())

		head := do(h, acceptingGzip(http.MethodHead, "/"))
		assert.Empty(t, head.Header().Get("Content-Encoding"))
	})

	t.Run("a stream flushed before anything is written is compressed", func(t *testing.T) {
		streaming := middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.Header().Set("Content-Type", "text/event-stream")
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte("data: 1\n\n"))
		}))

		rec := do(streaming, acceptingGzip(http.MethodGet, "/"))

		assert.Equal(t, "gzip", rec.Header().Get("Content-Encoding"))
		assert.Equal(t, "data: 1\n\n", unzip(t, rec))
	})

	t.Run("reaches the connection through the writer", func(t *testing.T) {
		var reached bool
		do(middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, reached = w.(interface{ Unwrap() http.ResponseWriter })
		})), acceptingGzip(http.MethodGet, "/"))

		assert.True(t, reached)
	})

	t.Run("many requests at once", func(t *testing.T) {
		// The compressors are pooled: a writer that kept state of one response would show in another.
		for i := range 50 {
			body := strings.Repeat(string(rune('a'+i%26)), 200+i)
			handler := middleware.Gzip(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(body))
			}))

			assert.Equal(t, body, unzip(t, do(handler, acceptingGzip(http.MethodGet, "/"))))
		}
	})
}

func TestGzipFiles(t *testing.T) {
	h := middleware.GzipFiles(".js", ".css")(page)

	for _, target := range []string{"/static/app.js", "/static/main.css", "/static/App.JS", "/static/js/chunks/a-1.js?v=x"} {
		assert.Equal(t, "gzip", do(h, acceptingGzip(http.MethodGet, target)).Header().Get("Content-Encoding"), target)
	}
	for _, target := range []string{"/static/logo.png", "/static/favicon.png", "/static/app.js.map", "/static/jsfile", "/"} {
		rec := do(h, acceptingGzip(http.MethodGet, target))

		assert.Empty(t, rec.Header().Get("Content-Encoding"), target)
		assert.Empty(t, rec.Header().Get("Vary"), "%s: what is left alone is not told apart", target)
	}
}
