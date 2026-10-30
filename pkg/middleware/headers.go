package middleware

import (
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/tung-dnt/lean-go/pkg/config"
	"github.com/tung-dnt/lean-go/pkg/htmx"
)

// Secure sets the headers that tell a browser not to do what it would do for an attacker: X-XSS-Protection to
// stop a page that it finds a script in, X-Content-Type-Options to stop it taking what is sent as an image for
// a script, and X-Frame-Options so that no other site can put a page in a frame. They are the three that Echo's
// Secure sent, with the values it sent.
func Secure(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-XSS-Protection", "1; mode=block")
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "SAMEORIGIN")

		next.ServeHTTP(w, r)
	})
}

// WithConfig puts the configuration in the context of every request, for config.FromContext, so that the page
// shell can read what it needs of it (the name of the site, the environment) without being handed it. Every
// route can end in the error page, the routes of files included, so it goes on all of them.
func WithConfig(cfg *config.Config) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			next.ServeHTTP(w, r.WithContext(config.NewContext(r.Context(), cfg)))
		})
	}
}

// CacheControl sets a Cache-Control header with a given max age: the browser and any cache may keep the
// response for that long, or for none (no-cache, no-store) when it is not positive.
func CacheControl(maxAge time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			v := "no-cache, no-store"
			if maxAge > 0 {
				v = fmt.Sprintf("public, max-age=%.0f", maxAge.Seconds())
			}
			w.Header().Set("Cache-Control", v)

			next.ServeHTTP(w, r)
		})
	}
}

// CacheBoostedPages marks boosted GET page responses as privately cacheable for
// maxAge. The htmx v4 preload extension can reuse its pending fetch on click;
// normal boosted requests can also reuse a fresh browser-cache response.
//
// Private (never a shared/CDN cache) because these pages are per-user. Scoped to
// boosted requests, which always render the full page (see ui.Render) —
// non-boosted htmx fragments are never cached, so a partial can't be served in
// place of a whole page. A maxAge of 0 disables it.
func CacheBoostedPages(maxAge time.Duration) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if maxAge > 0 && r.Method == http.MethodGet && htmx.GetRequest(r).Boosted {
				w.Header().Set("Cache-Control", fmt.Sprintf("private, max-age=%.0f", maxAge.Seconds()))
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RemoveTrailingSlash redirects "/notes/7/" to "/notes/7", for good (301), keeping the query. The router
// knows pages by one address, and a visitor, or a link someone else wrote, that adds a slash would be sent to
// the error page.
//
// It goes before the router, and a router has to have no pattern that ends in a slash for a page: the router
// redirects the other way, to add the slash to the root of such a pattern, and the two would send the visitor
// back and forth for ever.
func RemoveTrailingSlash(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		p := r.URL.Path
		if len(p) > 1 && strings.HasSuffix(p, "/") {
			uri := p[:len(p)-1]
			if r.URL.RawQuery != "" {
				uri += "?" + r.URL.RawQuery
			}

			w.Header().Set("Location", sanitizeURI(uri))
			w.WriteHeader(http.StatusMovedPermanently)

			return
		}

		next.ServeHTTP(w, r)
	})
}

// sanitizeURI keeps a redirect on the site. A double slash, "\\" or "\/" at the start of an address is an absolute
// address for a browser, so redirecting to "//evil.example" would send the visitor to another site; all of the
// slashes at the start become one.
func sanitizeURI(uri string) string {
	if len(uri) > 1 && (uri[0] == '\\' || uri[0] == '/') && (uri[1] == '\\' || uri[1] == '/') {
		return "/" + strings.TrimLeft(uri, `/\`)
	}

	return uri
}

// HTTPSRedirect redirects a request that came in over plain HTTP to the same address over HTTPS, for good (301).
// A request is over HTTPS when it came in over TLS, or when the proxy in front of the server says so
// (X-Forwarded-Proto and its relatives).
func HTTPSRedirect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isHTTPS(r) {
			next.ServeHTTP(w, r)

			return
		}

		w.Header().Set("Location", "https://"+r.Host+r.URL.RequestURI())
		w.WriteHeader(http.StatusMovedPermanently)
	})
}

// isHTTPS reports whether r came in over HTTPS.
func isHTTPS(r *http.Request) bool {
	switch {
	case r.TLS != nil:
		return true
	case r.Header.Get("X-Forwarded-Proto") == "https",
		r.Header.Get("X-Forwarded-Protocol") == "https",
		r.Header.Get("X-Forwarded-Ssl") == "on",
		r.Header.Get("X-Url-Scheme") == "https":
		return true
	}

	return false
}
