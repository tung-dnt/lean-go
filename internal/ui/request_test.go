package ui

import (
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"maragu.dev/gomponents"
	"maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/pkg/config"
	"github.com/tung-dnt/lean-go/pkg/htmx"
	"github.com/tung-dnt/lean-go/pkg/middleware"
	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/session"
)

func TestNewRequest(t *testing.T) {
	t.Run("a visitor who is not logged in", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		r := NewRequest(req)

		assert.Equal(t, req.Context(), r.Context)
		assert.Equal(t, "/", r.CurrentPath)
		assert.True(t, r.IsHome)
		assert.False(t, r.IsAuth)
		assert.False(t, r.IsAdmin)
		assert.Nil(t, r.AuthUser)
		assert.Equal(t, htmx.Request{}, r.Htmx)
		assert.NotNil(t, r.Config, "never nil, so that a page can always be drawn")
		assert.Empty(t, r.Config.App.Name)
	})

	t.Run("the user, the configuration and htmx", func(t *testing.T) {
		usr := &db.User{ID: 1, Admin: true}
		req := httptest.NewRequest(http.MethodGet, "/abc?page=2", nil)
		req.Header.Set(htmx.HeaderRequest, "true")
		req.Header.Set(htmx.HeaderBoosted, "true")
		cfg := &config.Config{App: config.AppConfig{Name: "testing"}}
		ctx := config.NewContext(middleware.WithUser(req.Context(), usr), cfg)

		r := NewRequest(req.WithContext(ctx))

		assert.Equal(t, "/abc", r.CurrentPath, "the path, and not the query")
		assert.False(t, r.IsHome)
		assert.True(t, r.IsAuth)
		assert.True(t, r.IsAdmin)
		assert.Same(t, usr, r.AuthUser)
		assert.Same(t, cfg, r.Config)
		assert.True(t, r.Htmx.Enabled)
		assert.True(t, r.Htmx.Boosted)
	})

	t.Run("a user who is not an admin", func(t *testing.T) {
		req := httptest.NewRequest(http.MethodGet, "/", nil)

		r := NewRequest(req.WithContext(middleware.WithUser(req.Context(), &db.User{ID: 2})))

		assert.True(t, r.IsAuth)
		assert.False(t, r.IsAdmin)
	})
}

func TestRequest_URL(t *testing.T) {
	r := NewRequest(httptest.NewRequest(http.MethodGet, "/", nil))
	r.Config = &config.Config{App: config.AppConfig{Host: "http://localhost"}}

	assert.Equal(t, "http://localhost/abc/123", r.URL("/abc/123"))
}

func TestRender(t *testing.T) {
	layout := func(_ *Request, n gomponents.Node) gomponents.Node {
		return html.Div(html.Class("test"), n)
	}
	node := html.P(gomponents.Text("hello"))

	render := func(t *testing.T, mutate func(*http.Request)) *httptest.ResponseRecorder {
		t.Helper()
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		if mutate != nil {
			mutate(req)
		}
		rec := httptest.NewRecorder()

		require.NoError(t, Render(rec, NewRequest(req), layout, node))

		return rec
	}

	t.Run("a page is in its layout", func(t *testing.T) {
		rec := render(t, nil)

		assert.Equal(t, `<div class="test"><p>hello</p></div>`, rec.Body.String())
		assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	})

	t.Run("an htmx request that is not boosted is answered with the node alone", func(t *testing.T) {
		rec := render(t, func(r *http.Request) { r.Header.Set(htmx.HeaderRequest, "true") })

		assert.Equal(t, `<p>hello</p>`, rec.Body.String())
	})

	t.Run("a boosted request is a whole page, whatever the layout", func(t *testing.T) {
		rec := render(t, func(r *http.Request) {
			r.Header.Set(htmx.HeaderRequest, "true")
			r.Header.Set(htmx.HeaderBoosted, "true")
		})

		assert.Equal(t, `<div class="test"><p>hello</p></div>`, rec.Body.String())
	})

	t.Run("keeps the content type that the handler chose", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.Header().Set("Content-Type", "text/html; charset=iso-8859-1")

		require.NoError(t, Render(rec, NewRequest(httptest.NewRequest(http.MethodGet, "/", nil)), layout, node))

		assert.Equal(t, "text/html; charset=iso-8859-1", rec.Header().Get("Content-Type"))
	})

	t.Run("the status is the handler's", func(t *testing.T) {
		rec := httptest.NewRecorder()
		rec.WriteHeader(http.StatusUnprocessableEntity)

		require.NoError(t, Render(rec, NewRequest(httptest.NewRequest(http.MethodGet, "/", nil)), layout, node))

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
	})

	t.Run("a page that cannot be built is an error, and nothing of it is written", func(t *testing.T) {
		boom := errors.New("boom")
		broken := gomponents.NodeFunc(func(w io.Writer) error {
			_, _ = w.Write([]byte("<p>half"))

			return boom
		})
		rec := httptest.NewRecorder()

		err := Render(rec, NewRequest(httptest.NewRequest(http.MethodGet, "/", nil)), layout, broken)

		require.ErrorIs(t, err, boom)
		assert.Empty(t, rec.Body.String(), "so that the caller can still send the error page")
		assert.Empty(t, rec.Header().Get("Content-Type"))
	})

	t.Run("a client that went away is an error", func(t *testing.T) {
		gone := errors.New("broken pipe")

		err := Render(failingWriter{httptest.NewRecorder(), gone}, NewRequest(httptest.NewRequest(http.MethodGet, "/", nil)), layout, node)

		assert.ErrorIs(t, err, gone)
	})
}

// failingWriter is a response writer whose body cannot be written.
type failingWriter struct {
	http.ResponseWriter
	err error
}

func (f failingWriter) Write([]byte) (int, error) { return 0, f.err }

func TestRenderStatus(t *testing.T) {
	layout := func(_ *Request, n gomponents.Node) gomponents.Node { return html.Div(n) }
	node := html.P(gomponents.Text("sorry"))

	t.Run("the status goes with the page", func(t *testing.T) {
		rec := httptest.NewRecorder()

		require.NoError(t, RenderStatus(rec, http.StatusUnprocessableEntity, NewRequest(httptest.NewRequest(http.MethodGet, "/", nil)), layout, node))

		assert.Equal(t, http.StatusUnprocessableEntity, rec.Code)
		assert.Equal(t, "<div><p>sorry</p></div>", rec.Body.String())
		assert.Equal(t, "text/html; charset=utf-8", rec.Header().Get("Content-Type"))
	})

	t.Run("the page is built before the status goes out, so what building it does to the session is saved", func(t *testing.T) {
		// A flash message is taken out of the session while the page is built, which is how it is shown once.
		// The cookie that says so is written with the headers, so the headers must not have gone out by then.
		store, err := session.NewStore(session.Config{Key: "test key"})
		require.NoError(t, err)
		flashing := func(r *Request, n gomponents.Node) gomponents.Node {
			var texts []gomponents.Node
			for _, m := range msg.Get(r.Context, msg.TypeSuccess) {
				texts = append(texts, gomponents.Text(m))
			}

			return html.Div(append(texts, n)...)
		}
		serve := func(handler http.HandlerFunc, cookies []*http.Cookie) (*httptest.ResponseRecorder, []*http.Cookie) {
			r := httptest.NewRequest(http.MethodGet, "/", nil)
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
		invalid := func(w http.ResponseWriter, r *http.Request) {
			require.NoError(t, RenderStatus(w, http.StatusUnprocessableEntity, NewRequest(r), flashing, node))
		}

		_, cookies := serve(func(_ http.ResponseWriter, r *http.Request) { msg.Success(r.Context(), "Saved.") }, nil)

		first, cookies := serve(invalid, cookies)
		assert.Contains(t, first.Body.String(), "Saved.")
		second, _ := serve(invalid, cookies)

		assert.NotContains(t, second.Body.String(), "Saved.", "it was shown once")
	})

	t.Run("a page that cannot be built has no status written", func(t *testing.T) {
		rec := httptest.NewRecorder()
		broken := gomponents.NodeFunc(func(io.Writer) error { return errors.New("boom") })

		err := RenderStatus(rec, http.StatusCreated, NewRequest(httptest.NewRequest(http.MethodGet, "/", nil)), layout, broken)

		require.Error(t, err)
		assert.False(t, rec.Flushed)
		assert.Empty(t, rec.Body.String())
	})
}
