package middleware_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/middleware"
)

func TestRecover(t *testing.T) {
	t.Run("a panic is a 500 with a line in the log that has the stack", func(t *testing.T) {
		c := newCapture()
		h := middleware.Recover(fail)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic("boom")
		}))

		rec := do(h, withLogger(t, c, httptest.NewRequest(http.MethodGet, "/", nil)))

		assert.Equal(t, http.StatusInternalServerError, rec.Code)
		records := c.all()
		require.Len(t, records, 1)
		assert.Equal(t, "boom", records[0].attrs["panic"])
		assert.Contains(t, records[0].attrs["stack"], "recover_test.go", "the stack is of where it happened")
	})

	t.Run("a handler that does not panic is not touched", func(t *testing.T) {
		rec := get(middleware.Recover(fail)(okHandler), "/")

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "ok", rec.Body.String())
	})

	t.Run("a panic after the response was started ends the connection", func(t *testing.T) {
		h := middleware.Chain(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("half a page"))
			panic("boom")
		}), middleware.LogRequests, middleware.Recover(fail))

		assert.PanicsWithValue(t, http.ErrAbortHandler, func() { get(h, "/") },
			"net/http aborts the connection on this one, and says nothing")
	})

	t.Run("a handler that wants the connection aborted gets it, with no stack in the log", func(t *testing.T) {
		c := newCapture()
		h := middleware.Recover(fail)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic(http.ErrAbortHandler)
		}))

		assert.PanicsWithValue(t, http.ErrAbortHandler, func() {
			do(h, withLogger(t, c, httptest.NewRequest(http.MethodGet, "/", nil)))
		})
		assert.Empty(t, c.all())
	})

	t.Run("a panic with an error is a 500 too", func(t *testing.T) {
		h := middleware.Recover(fail)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
			panic(errors.New("an error, and not a string"))
		}))

		assert.Equal(t, http.StatusInternalServerError, get(h, "/").Code)
	})
}
