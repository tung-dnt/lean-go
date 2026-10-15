package middleware_test

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/log"
)

// fail stands for the error page of the server, which this package may not import: it answers with the status
// that the error stands for, in plain text.
func fail(w http.ResponseWriter, r *http.Request, err error) {
	status := http.StatusInternalServerError

	var he *httperr.Error
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &he):
		status = he.Status
	case errors.As(err, &tooLarge):
		status = http.StatusRequestEntityTooLarge
	case errors.Is(context.Cause(r.Context()), httperr.ErrTimeout):
		status = http.StatusServiceUnavailable
	}

	http.Error(w, err.Error(), status)
}

// okHandler is a handler that answers 200 with "ok".
var okHandler = http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
	_, _ = w.Write([]byte("ok"))
})

// do runs the request through the handler, and returns what was answered.
func do(h http.Handler, r *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, r)

	return rec
}

// get is a GET of target through the handler.
func get(h http.Handler, target string) *httptest.ResponseRecorder {
	return do(h, httptest.NewRequest(http.MethodGet, target, nil))
}

// record is what a logger was told.
type record struct {
	level slog.Level
	msg   string
	attrs map[string]string
}

// capture is a slog.Handler that keeps what it is told, with the attributes of its With calls.
type capture struct {
	mu      *sync.Mutex
	records *[]record
	with    map[string]string
}

func newCapture() *capture {
	return &capture{mu: &sync.Mutex{}, records: &[]record{}, with: map[string]string{}}
}

func (c *capture) Enabled(context.Context, slog.Level) bool { return true }

func (c *capture) Handle(_ context.Context, r slog.Record) error {
	attrs := map[string]string{}
	for k, v := range c.with {
		attrs[k] = v
	}
	r.Attrs(func(a slog.Attr) bool {
		attrs[a.Key] = a.Value.String()

		return true
	})

	c.mu.Lock()
	defer c.mu.Unlock()
	*c.records = append(*c.records, record{level: r.Level, msg: r.Message, attrs: attrs})

	return nil
}

func (c *capture) WithAttrs(attrs []slog.Attr) slog.Handler {
	with := map[string]string{}
	for k, v := range c.with {
		with[k] = v
	}
	for _, a := range attrs {
		with[a.Key] = a.Value.String()
	}

	return &capture{mu: c.mu, records: c.records, with: with}
}

func (c *capture) WithGroup(string) slog.Handler { return c }

// all is what was logged so far.
func (c *capture) all() []record {
	c.mu.Lock()
	defer c.mu.Unlock()

	return append([]record(nil), *c.records...)
}

// withLogger is r with a context that carries a logger that writes to c.
func withLogger(t *testing.T, c *capture, r *http.Request) *http.Request {
	t.Helper()
	require.NotNil(t, c)

	return r.WithContext(log.NewContext(r.Context(), slog.New(c)))
}
