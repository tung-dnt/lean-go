package server

import (
	"fmt"
	"io/fs"
	"net/http"
	"os"

	"github.com/tung-dnt/lean-go/internal/auth"
	"github.com/tung-dnt/lean-go/internal/note"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/middleware"
	"github.com/tung-dnt/lean-go/pkg/paths"
	files "github.com/tung-dnt/lean-go/public"
)

// addRoutes is every route of the site, and the only place a route is registered. A pattern is the method and
// the path, with the parts of the path that change as {wildcards} that the handler reads with r.PathValue, and
// each path is also written in pkg/paths, which a link says instead of spelling it: paths_test.go holds
// the two to each other.
//
// Every route goes through the middleware of every page (NewServer). What a route adds to that is applied here,
// to that route: a guard (requireAuth), the cache of a file, the limits of a large body (middleware.LargeBody).
// Everything below /admin is held to the admin check by NewServer, so an admin route needs nothing here.
//
// The handlers are made of the dependencies they use, and of nothing else. A domain's routes go under its comment
// line, in the order of its pages, as the auth routes do.
func addRoutes(mux *http.ServeMux, d deps) error {
	fail := layouts.Error
	requireAuth := middleware.RequireAuth(fail)
	requireNoAuth := middleware.RequireNoAuth(fail)

	// Files: the embedded files of the site (they have the hash of their content in their address, so they
	// are kept for months, and the ones that compress are compressed), and the files of public/files.
	static, err := fs.Sub(files.Static, "static")
	if err != nil {
		return fmt.Errorf("the embedded files: %w", err)
	}
	keep := middleware.CacheControl(d.cfg.Cache.Expiration.PublicFile)
	mux.Handle("GET "+paths.StaticDir+"{path...}", middleware.Chain(HandleStatic(static), keep, middleware.GzipFiles(".js", ".css")))
	mux.Handle("GET "+paths.FilesDir+"{path...}", middleware.Chain(HandleStatic(os.DirFS("public/files")), keep))

	// Readiness: for the platform that runs the container, and for the test server.
	mux.Handle("GET "+paths.Healthz, HandleHealthz(d.pool))

	// Auth: how the admin logs in and out. There is one account, made by cmd/admin, so there is no registration and
	// no password reset.
	mux.Handle("GET "+paths.Login, requireNoAuth(auth.HandleLogin()))
	mux.Handle("POST "+paths.Login, requireNoAuth(auth.HandleLoginSubmit(d.queries)))
	mux.Handle("GET "+paths.Logout, requireAuth(auth.HandleLogout()))

	// Note: the example domain. The list is the root of the site, which is why its pattern ends in {$}: a pattern
	// for "/" alone would be every address that no other route takes. Anyone may post a note; deleting one is
	// below /admin, which NewServer holds to the admin check, so the route needs no guard of its own here.
	mux.Handle("GET "+paths.Home+"{$}", note.HandleList(d.queries))
	mux.Handle("POST "+paths.Notes, note.HandleCreate(d.queries))
	mux.Handle("GET /notes/{id}", note.HandleShow(d.queries))
	mux.Handle("POST /admin/notes/{id}/delete", note.HandleDelete(d.queries))

	// Whatever no route takes is a 404 with the error page.
	mux.Handle("/", HandleNotFound())

	return nil
}
