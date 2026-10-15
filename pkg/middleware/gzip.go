package middleware

import (
	"compress/gzip"
	"errors"
	"io"
	"net/http"
	"path"
	"strings"
	"sync"

	"github.com/tung-dnt/lean-go/pkg/log"
)

// Gzip compresses the response with gzip, for a client that says it takes it (Accept-Encoding).
//
// What it leaves alone: a response that has no body (a redirect, a 304), one that is already encoded, a 206 and
// anything for a request with a Range header, whose ranges are those of the uncompressed bytes, and the answer
// to a HEAD. The response is told apart from the same one uncompressed by Vary: Accept-Encoding, which is set
// on every response it looks at.
func Gzip(next http.Handler) http.Handler {
	return gzipHandler(next, nil)
}

// GzipFiles is Gzip for the requests of a path that ends in one of the extensions (".js", ".css"), and nothing
// for the others: the static files that compress, and not the images, which do not.
func GzipFiles(extensions ...string) Middleware {
	return func(next http.Handler) http.Handler {
		return gzipHandler(next, extensions)
	}
}

// gzipHandler is Gzip for the paths of the extensions, or for every path when there are none.
func gzipHandler(next http.Handler, extensions []string) http.Handler {
	writers := &sync.Pool{New: func() any { return gzip.NewWriter(io.Discard) }}

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if len(extensions) > 0 && !hasExtension(r.URL.Path, extensions) {
			next.ServeHTTP(w, r)

			return
		}

		w.Header().Add("Vary", "Accept-Encoding")
		if r.Method == http.MethodHead || r.Header.Get("Range") != "" || !acceptsGzip(r) {
			next.ServeHTTP(w, r)

			return
		}

		gw := &gzipWriter{ResponseWriter: w, pool: writers}
		defer gw.finish(r)

		next.ServeHTTP(gw, r)
	})
}

// hasExtension reports whether the path ends in one of the extensions.
func hasExtension(p string, extensions []string) bool {
	ext := path.Ext(p)

	for _, e := range extensions {
		if strings.EqualFold(ext, e) {
			return true
		}
	}

	return false
}

// acceptsGzip reports whether the client takes a gzip response.
func acceptsGzip(r *http.Request) bool {
	for _, part := range strings.Split(strings.Join(r.Header.Values("Accept-Encoding"), ","), ",") {
		name, params, _ := strings.Cut(strings.TrimSpace(part), ";")
		if !strings.EqualFold(strings.TrimSpace(name), "gzip") {
			continue
		}

		// "gzip;q=0" is a refusal.
		q := strings.ReplaceAll(strings.ToLower(params), " ", "")

		return q != "q=0" && q != "q=0.0" && q != "q=0.00" && q != "q=0.000"
	}

	return false
}

// gzipWriter compresses what the handler writes. Whether to compress is decided when the first byte of the body is
// written, which is the first time it is known that there is a body: the status that the handler chose is held
// back until then, and goes out plain when there is no body.
type gzipWriter struct {
	http.ResponseWriter
	pool *sync.Pool

	// zw is the compressor, once the body is being compressed.
	zw *gzip.Writer

	// status is the status the handler asked for, and has not gone out; headerSet says that it asked.
	status    int
	headerSet bool

	// decided is whether the first byte of the body has been seen and what to do with it settled.
	decided bool
}

// WriteHeader implements http.ResponseWriter.
func (gw *gzipWriter) WriteHeader(code int) {
	if gw.decided || (code >= 100 && code < 200 && code != http.StatusSwitchingProtocols) {
		gw.ResponseWriter.WriteHeader(code)

		return
	}
	if !gw.headerSet {
		gw.status = code
		gw.headerSet = true
	}
}

// Write implements http.ResponseWriter.
func (gw *gzipWriter) Write(b []byte) (int, error) {
	if !gw.decided {
		gw.decide(b, false)
	}
	if gw.zw != nil {
		return gw.zw.Write(b)
	}

	return gw.ResponseWriter.Write(b)
}

// decide settles whether the body is compressed, when its first bytes, b, are known, and lets the headers go. A
// flush before the first byte (streaming) is a body that is on its way, and is compressed as well.
func (gw *gzipWriter) decide(b []byte, streaming bool) {
	gw.decided = true

	h := gw.Header()
	status := gw.status
	if !gw.headerSet {
		status = http.StatusOK
	}

	compress := h.Get("Content-Encoding") == "" && status != http.StatusNoContent && status != http.StatusNotModified &&
		status != http.StatusPartialContent && (len(b) > 0 || streaming)
	if compress {
		// What net/http would have sniffed from the plain bytes is a gzip stream once they are compressed, so the
		// type has to be said now.
		if h.Get("Content-Type") == "" {
			h.Set("Content-Type", http.DetectContentType(b))
		}
		h.Set("Content-Encoding", "gzip")
		h.Del("Content-Length")

		gw.zw = gw.pool.Get().(*gzip.Writer)
		gw.zw.Reset(gw.ResponseWriter)
	}

	if gw.headerSet || compress {
		gw.ResponseWriter.WriteHeader(status)
	}
}

// Flush implements http.Flusher: what was compressed so far is sent.
func (gw *gzipWriter) Flush() {
	if !gw.decided {
		gw.decide(nil, true)
	}
	if gw.zw != nil {
		if err := gw.zw.Flush(); err != nil {
			log.Default().Debug("gzip: flush", "error", err)
		}
	}
	if err := http.NewResponseController(gw.ResponseWriter).Flush(); err != nil && !errors.Is(err, http.ErrNotSupported) {
		log.Default().Debug("gzip: flush", "error", err)
	}
}

// Unwrap lets http.ResponseController reach the writer beneath, for its deadlines and for hijacking.
func (gw *gzipWriter) Unwrap() http.ResponseWriter {
	return gw.ResponseWriter
}

// Written reports whether the response has been started, or the handler has chosen its status, since the
// status is not forwarded until the body starts.
func (gw *gzipWriter) Written() bool {
	return gw.headerSet || gw.decided
}

// finish ends the compressed stream, and lets out the status of a response that had no body.
func (gw *gzipWriter) finish(r *http.Request) {
	if gw.zw != nil {
		if err := gw.zw.Close(); err != nil {
			log.Ctx(r.Context()).Debug("gzip: close", "error", err)
		}
		gw.zw.Reset(io.Discard)
		gw.pool.Put(gw.zw)
		gw.zw = nil

		return
	}

	if !gw.decided && gw.headerSet {
		gw.ResponseWriter.WriteHeader(gw.status)
	}
}
