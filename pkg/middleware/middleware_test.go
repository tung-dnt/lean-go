package middleware_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/tung-dnt/lean-go/pkg/middleware"
)

// tag is a middleware that writes its name on the way in and on the way out.
func tag(name string, trail *[]string) middleware.Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			*trail = append(*trail, name+" in")
			next.ServeHTTP(w, r)
			*trail = append(*trail, name+" out")
		})
	}
}

func TestChain(t *testing.T) {
	var trail []string

	h := middleware.Chain(http.HandlerFunc(func(_ http.ResponseWriter, _ *http.Request) {
		trail = append(trail, "handler")
	}), tag("a", &trail), tag("b", &trail), tag("c", &trail))
	get(h, "/")

	assert.Equal(t, []string{"a in", "b in", "c in", "handler", "c out", "b out", "a out"}, trail, "the first is the outermost")

	assert.NotNil(t, middleware.Chain(okHandler), "no middleware is the handler")
}

func TestUnless(t *testing.T) {
	var trail []string
	skipStatic := middleware.Unless(func(r *http.Request) bool { return strings.HasPrefix(r.URL.Path, "/static/") }, tag("page", &trail))
	h := skipStatic(okHandler)

	get(h, "/login")
	assert.Equal(t, []string{"page in", "page out"}, trail)

	trail = nil
	rec := get(h, "/static/app.js")
	assert.Empty(t, trail, "skipped")
	assert.Equal(t, "ok", rec.Body.String(), "and still answered")
}

func TestUnder(t *testing.T) {
	var trail []string
	h := middleware.Under("/admin", tag("guard", &trail))(okHandler)

	for _, target := range []string{
		"/admin", "/admin/", "/admin/queue", "/admin/notes/1/delete", "/admin/uploads/x/y.png",
		"/admin/../admin/queue", "/admin/./queue", "/other/../admin/queue", "/admin//queue", "/admin?x=1",
	} {
		trail = nil
		get(h, target)
		assert.NotEmpty(t, trail, "%s is under /admin", target)
	}

	for _, target := range []string{"/", "/administrator", "/administrator/queue", "/Admin/queue", "/packs/admin", "/x/admin", "/adm", "/admin/../packs"} {
		trail = nil
		get(h, target)
		assert.Empty(t, trail, "%s is not under /admin", target)
	}
}

func TestUnder__EveryMethod(t *testing.T) {
	var trail []string
	h := middleware.Under("/admin", tag("guard", &trail))(okHandler)

	for _, method := range []string{http.MethodGet, http.MethodHead, http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete, http.MethodOptions} {
		trail = nil
		h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(method, "/admin/queue", nil))
		assert.NotEmpty(t, trail, method)
	}
}
