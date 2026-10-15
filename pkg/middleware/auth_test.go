package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/pkg/htmx"
	"github.com/tung-dnt/lean-go/pkg/middleware"
)

// asUser is a request from a visitor who is logged in as u.
func asUser(u *db.User) *http.Request {
	r := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)

	return r.WithContext(middleware.WithUser(r.Context(), u))
}

// guarded is the handler behind a guard, and whether it ran.
func guarded() (http.Handler, *bool) {
	ran := false

	return http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) { ran = true }), &ran
}

func TestUser(t *testing.T) {
	u := &db.User{ID: 4}

	got, ok := middleware.User(middleware.WithUser(t.Context(), u))
	assert.True(t, ok)
	assert.Same(t, u, got)

	_, ok = middleware.User(t.Context())
	assert.False(t, ok, "no one is logged in")

	_, ok = middleware.User(middleware.WithUser(t.Context(), nil))
	assert.False(t, ok, "a nil user is no user")
}

func TestRequireAuth(t *testing.T) {
	next, ran := guarded()
	h := middleware.RequireAuth(fail)(next)

	rec := get(h, "/logout")
	assert.Equal(t, http.StatusUnauthorized, rec.Code, "not logged in")
	assert.False(t, *ran)

	rec = do(h, asUser(&db.User{ID: 1}))
	assert.Equal(t, http.StatusOK, rec.Code, "logged in")
	assert.True(t, *ran)
}

func TestRequireNoAuth(t *testing.T) {
	next, ran := guarded()
	h := middleware.RequireNoAuth(fail)(next)

	rec := get(h, "/login")
	assert.Equal(t, http.StatusOK, rec.Code, "not logged in")
	assert.True(t, *ran)

	*ran = false
	rec = do(h, asUser(&db.User{ID: 1}))
	assert.Equal(t, http.StatusForbidden, rec.Code, "logged in")
	assert.False(t, *ran)
}

func TestRequireAdmin(t *testing.T) {
	next, ran := guarded()
	h := middleware.RequireAdmin("/login", fail)(next)

	t.Run("a visitor who is not logged in is sent to log in, and nothing behind it runs", func(t *testing.T) {
		*ran = false

		rec := get(h, "/admin/queue")

		assert.False(t, *ran)
		assert.Equal(t, http.StatusFound, rec.Code)
		assert.Equal(t, "/login", rec.Header().Get("Location"))
	})

	t.Run("the redirect is the same for a path that exists and one that does not", func(t *testing.T) {
		a, b := get(h, "/admin/queue"), get(h, "/admin/nothing/here")

		assert.Equal(t, a.Code, b.Code)
		assert.Equal(t, a.Header().Get("Location"), b.Header().Get("Location"))
	})

	t.Run("a request from a boosted link is not followed by the browser as a redirect: htmx is told instead", func(t *testing.T) {
		*ran = false
		r := httptest.NewRequest(http.MethodGet, "/admin/queue", nil)
		r.Header.Set(htmx.HeaderBoosted, "true")

		rec := do(h, r)

		assert.False(t, *ran)
		assert.Equal(t, "/login", rec.Header().Get(htmx.HeaderRedirect))
		assert.Empty(t, rec.Header().Get("Location"))
	})

	t.Run("logged in as a non-admin: refused outright, since logging in again would change nothing", func(t *testing.T) {
		*ran = false

		rec := do(h, asUser(&db.User{ID: 1, Admin: false}))

		assert.Equal(t, http.StatusForbidden, rec.Code)
		assert.False(t, *ran)
	})

	t.Run("logged in as an admin", func(t *testing.T) {
		*ran = false

		rec := do(h, asUser(&db.User{ID: 2, Admin: true}))

		assert.Equal(t, http.StatusOK, rec.Code)
		assert.True(t, *ran)
	})
}
