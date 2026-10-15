// Package server is the web server: Run builds everything it is made of and serves it, NewServer puts the
// routes behind the middleware every page goes through, and routes.go lists every route of the site in one
// place. There is no container and nothing registers itself: a domain's handlers are made from the concrete
// things they use, in routes.go, and that one file is where the site is read from.
package server

import (
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/config"
	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/middleware"
	"github.com/tung-dnt/lean-go/pkg/paths"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// deps is everything the routes are made of. routes.go hands each handler what it uses of it and nothing else,
// so a domain's port adds its routes there without changing the signature of NewServer or addRoutes.
type deps struct {
	cfg      *config.Config
	logger   *slog.Logger
	pool     *pgxpool.Pool
	queries  *db.Queries
	sessions *session.Store
}

// NewServer is the handler of the site: every route of routes.go, behind the middleware of every page.
//
// The pieces are what Run built, concrete, and each is the one thing: the logger, the pool and the queries over
// it, and the session store. It fails when something the handlers are made of cannot be made or the
// configuration cannot be served (an app.host that is no address).
func NewServer(
	cfg *config.Config,
	logger *slog.Logger,
	pool *pgxpool.Pool,
	queries *db.Queries,
	sessions *session.Store,
) (http.Handler, error) {
	d := deps{cfg: cfg, logger: logger, pool: pool, queries: queries, sessions: sessions}

	mux := http.NewServeMux()
	if err := addRoutes(mux, d); err != nil {
		return nil, err
	}

	origin, err := originOf(cfg.App.Host)
	if err != nil {
		return nil, err
	}
	// The site's own address is never another origin, whatever the Host that a proxy in front of it passes on.
	crossOrigin, err := middleware.CrossOrigin(layouts.Error, origin)
	if err != nil {
		return nil, fmt.Errorf("cross-origin protection: %w", err)
	}

	// What every page goes through, outermost first. The order matters, and each line says why.
	pages := []middleware.Middleware{
		// Before the router: the router knows a page by one address, and adds a slash to the root of a pattern
		// that ends in one, which would send a visitor back and forth with this.
		middleware.RemoveTrailingSlash,
		middleware.RequestID,
		middleware.Logger(logger),
		// Outside Recover, so that the access log sees the 500 that a panic comes to.
		middleware.LogRequests,
		middleware.Recover(layouts.Error),
		middleware.Gzip,
		// Outside everything that waits, and before LargeBody, which lifts it for a route that is allowed more.
		middleware.Timeout(cfg.App.Timeout),
		// Takes a copy of the request of its own before a body is read, so that the temporary files of a large
		// form are found and removed once the request is done.
		form.Cleanup,
		// A post from another site is refused here, before the handler or even the session looks at it.
		crossOrigin,
		sessions.Middleware,
		// After the session, which says who it is, and before every guard.
		middleware.LoadUser(queries, layouts.Error),
		// Everything below /admin is for the admin, a route nobody has written yet and a method nobody expects
		// included: the check is in front of the router, so it is the same for a page that is there and for one
		// that is not, and a new admin route is held to it from its first line.
		middleware.Under(paths.Admin, middleware.RequireAdmin(paths.Login, layouts.Error)),
		// Lets htmx-ext-preload reuse warmed boosted navigations on click.
		middleware.CacheBoostedPages(cfg.Cache.Expiration.Page),
	}
	page := func(next http.Handler) http.Handler { return middleware.Chain(next, pages...) }

	global := []middleware.Middleware{}
	if cfg.HTTP.TLS.Enabled {
		global = append(global, middleware.HTTPSRedirect)
	}
	global = append(global,
		// Every route, the routes of files included, can end in the error page, which reads the configuration
		// from the context of the request.
		middleware.WithConfig(cfg),
		// Likewise the logger: log.Ctx is this server's, not a process-wide default shared by every server.
		middleware.Logger(logger),
		middleware.Secure,
		// A file or a probe needs none of the above: no session, no user lookup, no cookie, no gzip of an image.
		middleware.Unless(isBare, page),
	)

	return middleware.Chain(mux, global...), nil
}

// isBare reports whether a request is for a file or a probe, which skips the middleware of the pages: the files
// of the site (/static and /files) and the readiness probe.
func isBare(r *http.Request) bool {
	p := r.URL.Path

	return strings.HasPrefix(p, paths.StaticDir) || strings.HasPrefix(p, paths.FilesDir) || p == paths.Healthz
}

// originOf is the origin, "https://example.com", of the address the site says it has (config app.host).
func originOf(host string) (string, error) {
	u, err := url.Parse(host)
	if err != nil || u.Scheme == "" || u.Host == "" {
		return "", fmt.Errorf("config app.host %q is not an address with a scheme and a host, such as http://localhost:8000", host)
	}

	return u.Scheme + "://" + u.Host, nil
}
