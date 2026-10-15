package layouts_test

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/config"
	"github.com/tung-dnt/lean-go/pkg/htmx"
	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/log"
	"github.com/tung-dnt/lean-go/pkg/middleware"
	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// logs keeps what a logger was told, by level.
type logs struct {
	mu      sync.Mutex
	entries []string
}

func (l *logs) Enabled(context.Context, slog.Level) bool { return true }
func (l *logs) Handle(_ context.Context, r slog.Record) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.entries = append(l.entries, r.Level.String()+" "+r.Message)

	return nil
}
func (l *logs) WithAttrs([]slog.Attr) slog.Handler { return l }
func (l *logs) WithGroup(string) slog.Handler      { return l }

func (l *logs) all() []string {
	l.mu.Lock()
	defer l.mu.Unlock()

	return append([]string(nil), l.entries...)
}

// answer runs err through the error page for a request of the site and returns what was answered, and what was
// logged.
func answer(t *testing.T, err error, mutate ...func(*http.Request)) (*httptest.ResponseRecorder, []string) {
	t.Helper()

	captured := &logs{}
	ctx := config.NewContext(log.NewContext(t.Context(), slog.New(captured)), &config.Config{App: config.AppConfig{Name: "leango"}})
	r := httptest.NewRequest(http.MethodGet, "/somewhere", nil).WithContext(ctx)
	for _, m := range mutate {
		m(r)
	}
	rec := httptest.NewRecorder()

	layouts.Error(rec, r, err)

	return rec, captured.all()
}

func TestError__TheStatusIsTheOneTheErrorStandsFor(t *testing.T) {
	for name, tc := range map[string]struct {
		err    error
		status int
		says   string
	}{
		"a 404":                 {httperr.New(http.StatusNotFound, "no such note"), http.StatusNotFound, "to go return home"},
		"a 403":                 {httperr.New(http.StatusForbidden, ""), http.StatusForbidden, "You are not authorized to view the requested page."},
		"a 401":                 {httperr.New(http.StatusUnauthorized, ""), http.StatusUnauthorized, "You are not authorized to view the requested page."},
		"a 500":                 {httperr.New(http.StatusInternalServerError, "boom"), http.StatusInternalServerError, "Please try again."},
		"a 400":                 {httperr.New(http.StatusBadRequest, "bad form"), http.StatusBadRequest, "Something went wrong."},
		"a 503":                 {httperr.New(http.StatusServiceUnavailable, ""), http.StatusServiceUnavailable, "Something went wrong."},
		"a 409":                 {httperr.New(http.StatusConflict, ""), http.StatusConflict, "Something went wrong."},
		"a 413 with its words":  {httperr.New(http.StatusRequestEntityTooLarge, "That upload is too large. An upload can be up to 100 MB in all."), http.StatusRequestEntityTooLarge, "That upload is too large. An upload can be up to 100 MB in all."},
		"a 413 with none":       {httperr.New(http.StatusRequestEntityTooLarge, ""), http.StatusRequestEntityTooLarge, "That request is too large."},
		"a body over the limit": {fmt.Errorf("form: %w", &http.MaxBytesError{Limit: 100 << 20}), http.StatusRequestEntityTooLarge, "It can be up to 100 MB."},
		"a plain error":         {errors.New("the database is on fire"), http.StatusInternalServerError, "Please try again."},
		"a wrapped status":      {fmt.Errorf("handler: %w", httperr.New(http.StatusNotFound, "")), http.StatusNotFound, "to go return home"},
		"a deadline":            {fmt.Errorf("query: %w", context.DeadlineExceeded), http.StatusServiceUnavailable, "Something went wrong."},
	} {
		rec, _ := answer(t, tc.err)

		assert.Equal(t, tc.status, rec.Code, name)
		assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"), name)
		assert.Contains(t, rec.Body.String(), tc.says, name)
		assert.Contains(t, rec.Body.String(), "<title>leango | "+http.StatusText(tc.status)+"</title>", "%s: the page is titled by the status", name)
		assert.Contains(t, rec.Body.String(), "<h1>"+http.StatusText(tc.status)+"</h1>", name)
	}
}

func TestError__TheTextOfAnErrorIsNeverShownExceptWhatA413Says(t *testing.T) {
	for _, status := range []int{http.StatusBadRequest, http.StatusForbidden, http.StatusNotFound, http.StatusInternalServerError, http.StatusServiceUnavailable} {
		rec, _ := answer(t, httperr.New(status, "secret detail: password=hunter2"))

		assert.NotContains(t, rec.Body.String(), "hunter2", http.StatusText(status))
	}
	rec, _ := answer(t, errors.New("pq: password authentication failed for user leango"))
	assert.NotContains(t, rec.Body.String(), "leango:", "an error from below is for the log")
	assert.NotContains(t, rec.Body.String(), "authentication failed")
}

func TestError__TheNotFoundPageLinksHome(t *testing.T) {
	rec, _ := answer(t, httperr.New(http.StatusNotFound, ""))

	assert.Contains(t, rec.Body.String(), `<a href="`+paths.Home+`">here</a>`)
}

func TestError__AFiveHundredIsLoggedAsAnErrorAndAFourHundredAsAWarning(t *testing.T) {
	_, logged := answer(t, errors.New("boom"))
	require.Len(t, logged, 1)
	assert.Equal(t, "ERROR boom", logged[0])

	_, logged = answer(t, httperr.New(http.StatusNotFound, "no such note"))
	require.Len(t, logged, 1)
	assert.Equal(t, "WARN 404 Not Found: no such note", logged[0])

	_, logged = answer(t, httperr.New(http.StatusServiceUnavailable, ""))
	assert.Equal(t, []string{"ERROR 503 Service Unavailable"}, logged)
}

func TestError__ARequestThatRanOutOfTimeIsAServiceUnavailable(t *testing.T) {
	// What a cancelled query returns says nothing of why: the reason is the cause of the context, which the
	// middleware that cancelled it gave.
	ctx, cancel := context.WithCancelCause(t.Context())
	cancel(httperr.ErrTimeout)

	rec, _ := answer(t, fmt.Errorf("query: %w", context.Canceled), func(r *http.Request) {
		*r = *r.WithContext(
			config.NewContext(log.NewContext(ctx, log.Default()), &config.Config{}))
	})

	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
}

func TestError__ARequestThatTheClientLeftIsNotAnswered(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	rec, logged := answer(t, fmt.Errorf("query: %w", context.Canceled), func(r *http.Request) {
		*r = *r.WithContext(
			config.NewContext(log.NewContext(ctx, slog.New(&logs{})), &config.Config{}))
	})

	assert.Empty(t, rec.Body.String(), "there is nobody to read it")
	assert.Empty(t, logged, "and it is no error of the server's")
	assert.False(t, rec.Flushed)
}

func TestError__HTMXAsksForAFragmentAndAPageIsNotWhatItGets(t *testing.T) {
	setHTMX := func(boosted bool) func(*http.Request) {
		return func(r *http.Request) {
			r.Header.Set(htmx.HeaderRequest, "true")
			if boosted {
				r.Header.Set(htmx.HeaderBoosted, "true")
			}
		}
	}

	fragment, _ := answer(t, httperr.New(http.StatusInternalServerError, ""), setHTMX(false))
	assert.Equal(t, http.StatusInternalServerError, fragment.Code)
	assert.Equal(t, "<p>Please try again.</p>", fragment.Body.String(), "the node and no layout: it is swapped into a page that is there")

	boosted, _ := answer(t, httperr.New(http.StatusInternalServerError, ""), setHTMX(true))
	assert.Contains(t, boosted.Body.String(), "<!doctype html>", "a boosted request replaces the page")
}

func TestError__AResponseThatHasStartedCannotBecomeAnErrorPage(t *testing.T) {
	captured := &logs{}
	ctx := config.NewContext(log.NewContext(t.Context(), slog.New(captured)), &config.Config{})
	handler := middleware.LogRequests(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("half a page"))
		layouts.Error(w, r, errors.New("boom"))
	}))
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil).WithContext(ctx))

	assert.Equal(t, http.StatusOK, rec.Code)
	assert.Equal(t, "half a page", rec.Body.String(), "nothing was added to what was sent")
	assert.True(t, strings.Contains(strings.Join(captured.all(), "\n"), "ERROR boom"), "but the error was logged")
}

// The page of an error is drawn in the layout of the site, flash messages included, and the messages it takes
// out of the session are not there on the next page: they were shown.
func TestError__FlashMessagesAreShownOnceEvenOnTheErrorPage(t *testing.T) {
	store, err := session.NewStore(session.Config{Key: "test key"})
	require.NoError(t, err)
	cfg := &config.Config{App: config.AppConfig{Name: "leango"}}
	serve := func(handler http.HandlerFunc, cookies []*http.Cookie) (*httptest.ResponseRecorder, []*http.Cookie) {
		r := httptest.NewRequest(http.MethodGet, "/", nil).WithContext(config.NewContext(t.Context(), cfg))
		for _, c := range cookies {
			r.AddCookie(c)
		}
		rec := httptest.NewRecorder()
		store.Middleware(handler).ServeHTTP(rec, r)
		if set := rec.Result().Cookies(); len(set) > 0 {
			return rec, set
		}

		return rec, cookies
	}
	notFound := func(w http.ResponseWriter, r *http.Request) {
		layouts.Error(w, r, httperr.New(http.StatusNotFound, ""))
	}

	_, cookies := serve(func(_ http.ResponseWriter, r *http.Request) { msg.Success(r.Context(), "Saved.") }, nil)

	first, cookies := serve(notFound, cookies)
	assert.Contains(t, first.Body.String(), "Saved.", "the error page shows the message")

	second, _ := serve(notFound, cookies)
	assert.NotContains(t, second.Body.String(), "Saved.", "and it is not shown again")
}
