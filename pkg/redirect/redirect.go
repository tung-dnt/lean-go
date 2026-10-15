// Package redirect sends the browser elsewhere in the way the request asked for.
package redirect

import (
	"net/http"

	"github.com/tung-dnt/lean-go/pkg/htmx"
)

// To redirects the request to dest, a path of the site (take it from internal/server/paths) or an absolute URL, with
// the status code, one of the 3xx: http.StatusSeeOther after a POST, so that a form sent without htmx is
// answered with a GET of the page and not with a second POST, http.StatusFound or http.StatusTemporaryRedirect
// for a GET.
//
// A request from a boosted link or form (hx-boost) is not given the redirect: htmx follows a redirect inside
// its own request and would swap the page of the destination in, but the address bar would stay where it was.
// It is answered with HX-Redirect instead, which makes htmx navigate to dest as a browser would, and the status
// is a 200 whatever code was asked for.
func To(w http.ResponseWriter, r *http.Request, dest string, code int) {
	if htmx.GetRequest(r).Boosted {
		htmx.Response{Redirect: dest}.Apply(w)

		return
	}

	w.Header().Set("Location", dest)
	w.WriteHeader(code)
}
