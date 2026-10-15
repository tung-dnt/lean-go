package middleware_test

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/middleware"
)

// slowHandler waits for 600 ms, as a query that is slow does, and gives up when its context is done, as a query
// that takes the context does: a request that is cancelled is answered with the reason, which fail turns into
// the status.
var slowHandler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
	select {
	case <-time.After(600 * time.Millisecond):
		_, _ = w.Write([]byte("done"))
	case <-r.Context().Done():
		fail(w, r, r.Context().Err())
	}
})

func TestTimeout__EveryRequestHasItsTimeAndAFileUploadGetsItsOwn(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("GET /page", slowHandler)
	mux.Handle("POST /other", slowHandler)
	mux.Handle("GET /upload", slowHandler)
	mux.Handle("POST /upload", middleware.LargeBody(1<<20, 3*time.Second, fail)(slowHandler))
	h := middleware.Timeout(300 * time.Millisecond)(mux)

	serve := func(method, target string) int {
		return do(h, httptest.NewRequest(method, target, nil)).Code
	}

	// Every request has app.timeout, except the large-body request, which has its own.
	assert.Equal(t, http.StatusServiceUnavailable, serve(http.MethodGet, "/page"))
	assert.Equal(t, http.StatusServiceUnavailable, serve(http.MethodPost, "/other"))
	assert.Equal(t, http.StatusServiceUnavailable, serve(http.MethodGet, "/upload"), "the form is a page like any other")
	assert.Equal(t, http.StatusOK, serve(http.MethodPost, "/upload"))
}

func TestTimeout__AnUnsetUploadTimeoutDoesNotMeanNoTimeout(t *testing.T) {
	mux := http.NewServeMux()
	mux.Handle("POST /upload", middleware.LargeBody(1<<20, 0, fail)(slowHandler))
	h := middleware.Timeout(300 * time.Millisecond)(mux)

	rec := do(h, httptest.NewRequest(http.MethodPost, "/upload", nil))

	// A limit of zero must not lift the one that everything has.
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestTimeout__TheContextIsCancelledForThatReason(t *testing.T) {
	var cause, err error
	h := middleware.Timeout(50 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
		cause, err = context.Cause(r.Context()), r.Context().Err()
	}))

	get(h, "/")

	assert.ErrorIs(t, cause, httperr.ErrTimeout, "that is what the error page tells a timeout from a client that went away by")
	assert.ErrorIs(t, err, context.Canceled, "and what a query that was cancelled returns says nothing of why")
}

func TestTimeout__ARequestThatIsFastEnoughIsNotTouched(t *testing.T) {
	var err error
	h := middleware.Timeout(time.Minute)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("fast"))
		err = r.Context().Err()
	}))

	rec := get(h, "/")

	assert.Equal(t, "fast", rec.Body.String())
	assert.NoError(t, err)
}

func TestTimeout__AClientThatLeavesIsNotATimeout(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	var cause error
	h := middleware.Timeout(time.Minute)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
		cancel()
		cause = context.Cause(r.Context())
	}))

	do(h, httptest.NewRequestWithContext(ctx, http.MethodGet, "/", nil))

	assert.ErrorIs(t, cause, context.Canceled)
	assert.False(t, errors.Is(cause, httperr.ErrTimeout))
}

func TestTimeout__NoLimitIsNoLimit(t *testing.T) {
	for _, d := range []time.Duration{0, -time.Second} {
		var deadline bool
		var extended bool
		h := middleware.Timeout(d)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			time.Sleep(20 * time.Millisecond)
			deadline = r.Context().Err() != nil
			extended = middleware.ExtendTimeout(r.Context(), time.Second)
		}))

		get(h, "/")

		assert.False(t, deadline, d)
		assert.False(t, extended, "%v: there is no deadline to move", d)
	}
}

func TestExtendTimeout(t *testing.T) {
	t.Run("moves the deadline", func(t *testing.T) {
		var err error
		var moved bool
		h := middleware.Timeout(50 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			moved = middleware.ExtendTimeout(r.Context(), 2*time.Second)
			time.Sleep(150 * time.Millisecond)
			err = r.Context().Err()
		}))

		get(h, "/")

		assert.True(t, moved)
		assert.NoError(t, err, "the first deadline passed and the request was not cancelled")
	})

	t.Run("is not a way to take time back after it was lost", func(t *testing.T) {
		var moved bool
		h := middleware.Timeout(20 * time.Millisecond)(http.HandlerFunc(func(_ http.ResponseWriter, r *http.Request) {
			<-r.Context().Done()
			moved = middleware.ExtendTimeout(r.Context(), time.Hour)
		}))

		get(h, "/")

		assert.False(t, moved, "a request that has timed out is over")
	})

	t.Run("there is nothing to move without Timeout", func(t *testing.T) {
		assert.False(t, middleware.ExtendTimeout(t.Context(), time.Second))
	})
}

// slowBody is a request body that arrives one byte per chunk, with a pause before every chunk but the
// first, so that sending it takes about (chunks-1)*pause. A pipe has no known length, which makes the client
// send it chunked: nothing is announced up front, and it is all read as it comes.
func slowBody(chunks int, pause time.Duration) io.Reader {
	pr, pw := io.Pipe()
	go func() {
		for i := range chunks {
			if i > 0 {
				time.Sleep(pause)
			}
			// The client closing its end of the pipe, once it has given up, ends the loop.
			if _, err := pw.Write([]byte("x")); err != nil {
				return
			}
		}
		if err := pw.Close(); err != nil {
			return
		}
	}()

	return pr
}

// deadlineServer serves two routes that read their whole body and answer with its length after a pause of
// their own. The server's ReadTimeout and WriteTimeout are limit, as in production they are seconds. Only
// /slow is a large body, and has timeout to read and to answer in.
func deadlineServer(t *testing.T, limit, timeout time.Duration) *httptest.Server {
	t.Helper()

	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			fail(w, r, err)

			return
		}
		// The write deadline is counted from the headers, not from the end of the body, so a route that
		// answers after the limit has passed is one that only an extended deadline can answer.
		time.Sleep(limit + limit/2)

		_, _ = w.Write([]byte(strconv.FormatInt(n, 10)))
	})

	mux := http.NewServeMux()
	mux.Handle("POST /slow", middleware.LargeBody(1<<20, timeout, fail)(read))
	mux.Handle("POST /fast", read)

	srv := httptest.NewUnstartedServer(middleware.Timeout(time.Minute)(mux))
	srv.Config.ReadTimeout = limit
	srv.Config.WriteTimeout = limit
	srv.Start()
	t.Cleanup(srv.Close)

	return srv
}

// post sends body to path and reports what came of it: the status and the answer, or the error that ended
// the exchange. A server that cuts a connection off leaves the client with an error, or with whatever it
// managed to write first.
func post(t *testing.T, srv *httptest.Server, path string, body io.Reader) (status int, answer string, err error) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodPost, srv.URL+path, body)
	require.NoError(t, err)

	client := &http.Client{Timeout: 30 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return 0, "", err
	}

	b, err := io.ReadAll(resp.Body)
	if cerr := resp.Body.Close(); err == nil {
		err = cerr
	}

	return resp.StatusCode, string(b), err
}

func TestLargeBody__TheRouteOutlivesTheServerLimits(t *testing.T) {
	const limit = 300 * time.Millisecond
	srv := deadlineServer(t, limit, time.Minute)

	// Four chunks, 250ms apart: about 750ms to arrive, then 450ms before the answer. Both are well past
	// what the server itself allows.
	status, answer, err := post(t, srv, "/slow", slowBody(4, 250*time.Millisecond))

	require.NoError(t, err)
	assert.Equal(t, http.StatusOK, status)
	assert.Equal(t, "4", answer, "the whole body arrived")
}

func TestLargeBody__OtherRoutesKeepTheServerLimits(t *testing.T) {
	const limit = 300 * time.Millisecond
	srv := deadlineServer(t, limit, time.Minute)

	// The very same request to a route that is not a large body: the control that makes the test above mean
	// something, and the guarantee that a slow client gains nothing anywhere else.
	status, _, err := post(t, srv, "/fast", slowBody(4, 250*time.Millisecond))

	assert.True(t, err != nil || status != http.StatusOK, "the server's own limit must still cut it off (status %d)", status)
}

func TestLargeBody__TheExtensionIsBounded(t *testing.T) {
	const limit = 300 * time.Millisecond
	srv := deadlineServer(t, limit, 700*time.Millisecond)

	// Seven chunks, 300ms apart, take 1.8s to arrive: more than the extension. It moves the deadline, it
	// does not remove it.
	status, _, err := post(t, srv, "/slow", slowBody(7, 300*time.Millisecond))

	assert.True(t, err != nil || status != http.StatusOK, "a request past the extended deadline must be cut off (status %d)", status)
}

func TestLargeBody__ZeroChangesNothing(t *testing.T) {
	const limit = 300 * time.Millisecond
	srv := deadlineServer(t, limit, 0)

	status, _, err := post(t, srv, "/slow", slowBody(4, 250*time.Millisecond))

	assert.True(t, err != nil || status != http.StatusOK, "an unset extension must not lift the server's limit (status %d)", status)
}

func TestLargeBody__WithoutAConnectionThereIsNothingToExtend(t *testing.T) {
	// The recorder, like the writer of a test that calls the handler directly, is not backed by a connection.
	// That must not stop the request.
	called := false
	h := middleware.LargeBody(1<<20, time.Minute, fail)(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		called = true
	}))

	rec := do(h, httptest.NewRequest(http.MethodPost, "/upload", strings.NewReader("body")))

	assert.True(t, called)
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestLargeBody__TheBodyIsCapped(t *testing.T) {
	const limit = 100
	read := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n, err := io.Copy(io.Discard, r.Body)
		if err != nil {
			fail(w, r, err)

			return
		}
		_, _ = w.Write([]byte(strconv.FormatInt(n, 10)))
	})
	h := middleware.LargeBody(limit, 0, fail)(read)

	t.Run("a body that fits is read", func(t *testing.T) {
		rec := do(h, httptest.NewRequest(http.MethodPost, "/", strings.NewReader(strings.Repeat("x", limit))))

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.Equal(t, "100", rec.Body.String())
	})

	t.Run("a body that announces more is refused before a byte is read", func(t *testing.T) {
		body := &countingReader{r: strings.NewReader(strings.Repeat("x", limit+1))}
		r := httptest.NewRequest(http.MethodPost, "/", body)
		r.ContentLength = limit + 1

		rec := do(h, r)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
		assert.Zero(t, body.n, "nothing of it was read")
	})

	t.Run("a body that does not say how big it is, and goes over, is cut off", func(t *testing.T) {
		r := httptest.NewRequest(http.MethodPost, "/", io.NopCloser(strings.NewReader(strings.Repeat("x", 10*limit))))
		r.ContentLength = -1

		rec := do(h, r)

		assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	})
}

// countingReader counts the bytes read from it.
type countingReader struct {
	r io.Reader
	n int
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	c.n += n

	return n, err
}
