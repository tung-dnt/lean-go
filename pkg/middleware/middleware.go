// Package middleware is what the server does to a request before and after the handler that answers it: it
// numbers it, logs it, turns a panic into a 500, loads the session and the user, refuses the one that may not be
// there, and so on.
//
// A middleware is a func(http.Handler) http.Handler, so any middleware written for net/http can be mixed with
// these, and Chain puts them in order. Those that may have to refuse a request do not draw the error page, which
// the page shell owns and this package may not import; they take an ErrorFunc, which the server hands them
// (layouts.Error), and tell it what was wrong. Everything that a middleware leaves for the handlers to read, the
// logger, the session, the user, the configuration, is put in the context of the request, and read from it with
// a typed accessor, never by a string key.
//
// The page that a handler writes passes through the middleware outermost first, in the order of internal/server's
// NewServer; read it there to see which comes before which, because some depend on it: the session has to be
// loaded before the user is, and the timeout has to come before whatever LargeBody lifts it for.
package middleware

import (
	"net/http"
	"path"
	"strings"
)

// Middleware wraps a handler in something that happens before and after it.
type Middleware = func(http.Handler) http.Handler

// ErrorFunc answers a request that cannot be served, with the error that says why (an *httperr.Error for a status
// of its own, anything else for a 500). The server's is layouts.Error, which draws the error page.
type ErrorFunc = func(w http.ResponseWriter, r *http.Request, err error)

// Chain is h wrapped in the middleware, the first of them outermost: Chain(h, a, b) runs a, then b, then h, and
// the response comes back through b and then a.
func Chain(h http.Handler, mws ...Middleware) http.Handler {
	for i := len(mws) - 1; i >= 0; i-- {
		h = mws[i](h)
	}

	return h
}

// Unless is mw for every request but those that skip picks out, which go to the handler as they are.
func Unless(skip func(*http.Request) bool, mw Middleware) Middleware {
	return func(next http.Handler) http.Handler {
		wrapped := mw(next)

		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if skip(r) {
				next.ServeHTTP(w, r)

				return
			}
			wrapped.ServeHTTP(w, r)
		})
	}
}

// Under is mw for the requests at or below the path prefix ("/admin": "/admin" and "/admin/anything"), and
// nothing for the others. It holds a whole part of the site to a rule, a route that nobody has written yet
// included, which is not what a middleware put on each route does.
//
// The path is cleaned first, so "/admin/../admin/queue" is below /admin as it will be once the router has
// redirected it.
func Under(prefix string, mw Middleware) Middleware {
	return Unless(func(r *http.Request) bool {
		p := path.Clean("/" + r.URL.Path)

		return p != prefix && !strings.HasPrefix(p, prefix+"/")
	}, mw)
}
