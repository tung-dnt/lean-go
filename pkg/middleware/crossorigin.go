package middleware

import (
	"net/http"

	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/log"
)

// CrossOrigin refuses, with a 403, a request that a page of another site made on behalf of the visitor, which is
// what makes a post to this site from a site that the visitor has open in another tab a forgery. It is
// http.CrossOriginProtection: a request that is not safe (not a GET, HEAD or OPTIONS) is refused when the
// browser says it comes from another origin, by its Sec-Fetch-Site header or, for a browser that sends none, by
// an Origin header that is not the Host. A request with neither header is a request that is not from a
// browser, and goes through: there is no session of a visitor to forge from a client that is not one.
//
// It is what the CSRF tokens of the forms used to be, and they are gone: no form carries one. trusted are the
// origins, "https://example.com", that are let through whatever the browser says, which the site's own address
// is when a proxy changes the Host.
//
// An origin that cannot be one is an error.
func CrossOrigin(fail ErrorFunc, trusted ...string) (Middleware, error) {
	protection := http.NewCrossOriginProtection()
	for _, origin := range trusted {
		if err := protection.AddTrustedOrigin(origin); err != nil {
			return nil, err
		}
	}

	protection.SetDenyHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Why it was refused is for the log, and not for the page, which says the same to anyone.
		reason := protection.Check(r)
		log.Ctx(r.Context()).Warn("cross-origin request refused", "error", reason, "origin", r.Header.Get("Origin"))

		fail(w, r, httperr.New(http.StatusForbidden, "cross-origin request refused"))
	}))

	return protection.Handler, nil
}
