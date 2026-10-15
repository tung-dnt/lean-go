package server

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/pkg/config"
)

// route is a path of internal/server/paths, which has to lead to a route of routes.go, with the method the site asks it with.
type route struct {
	method string
	path   string
}

// routes lists every path that internal/server/paths has, with a value for each of its parameters. A path that is added
// there is added here, and the test below holds the two to the routes that routes.go registers.
var routes = []route{
	{http.MethodGet, paths.Healthz},
	{http.MethodGet, paths.StaticDir + "main.css"},
	{http.MethodGet, paths.FilesDir + "a/b.png"},

	{http.MethodGet, paths.Login},
	{http.MethodPost, paths.Login},
	{http.MethodGet, paths.Logout},

	{http.MethodGet, paths.Home},
	{http.MethodPost, paths.Notes},
	{http.MethodGet, paths.Note(7)},
	{http.MethodPost, paths.AdminNoteDelete(7)},
}

// routesOf are the routes of the site. No handler runs, so the configuration is all they are made of here.
func routesOf(t *testing.T) *http.ServeMux {
	t.Helper()

	cfg, err := config.Load(func(string) string { return "" }, "../../config")
	require.NoError(t, err)

	mux := http.NewServeMux()
	require.NoError(t, addRoutes(mux, deps{cfg: cfg}))

	return mux
}

// A path that does not lead to a route is a link to the error page: the test finds out which, whatever
// the page that carries it.
func TestPaths__EveryPathLeadsToARoute(t *testing.T) {
	mux := routesOf(t)

	for _, r := range routes {
		_, pattern := mux.Handler(httptest.NewRequest(r.method, r.path, nil))

		assert.True(t, strings.HasPrefix(pattern, r.method+" "), "%s %s leads to %q, which is not a route of its own (nor of its method)", r.method, r.path, pattern)
	}
}

// The patterns that routes.go registers are the paths of internal/server/paths: the router reads a {wildcard} where a
// path has a value, and the two must agree on everything else.
func TestPaths__TheWildcardsAreTheValuesOfTheParameters(t *testing.T) {
	mux := routesOf(t)

	for pattern, path := range map[string]string{
		"GET /{$}":                      paths.Home,
		"GET /notes/{id}":               paths.Note(7),
		"POST /admin/notes/{id}/delete": paths.AdminNoteDelete(7),
	} {
		method, _, _ := strings.Cut(pattern, " ")
		_, got := mux.Handler(httptest.NewRequest(method, path, nil))

		assert.Equal(t, pattern, got, "%s: the handler reads the wildcard by this name", path)
	}
}
