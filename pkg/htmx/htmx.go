// Package htmx reads what htmx says about the request it sent, and says to htmx what to do with the response.
package htmx

import (
	"net/http"
)

// Request headers: https://four.htmx.org/reference/headers/HX-Source
const (
	HeaderBoosted     = "HX-Boosted"
	HeaderRequest     = "HX-Request"
	HeaderRequestType = "HX-Request-Type"
	HeaderSource      = "HX-Source"
	HeaderTarget      = "HX-Target"
)

// Response headers: https://four.htmx.org/docs/whats-new-in-htmx-4#response-headers
const (
	HeaderPushURL    = "HX-Push-Url"
	HeaderRedirect   = "HX-Redirect"
	HeaderReplaceURL = "HX-Replace-Url"
	HeaderRefresh    = "HX-Refresh"
	HeaderTrigger    = "HX-Trigger"
)

type (
	// Request contains data that HTMX provides during requests.
	Request struct {
		Enabled bool
		Boosted bool
		// Source and Target use htmx v4 identifiers, such as "button#submit".
		Source      string
		Target      string
		RequestType string
	}

	// Response contain data that the server can communicate back to HTMX.
	Response struct {
		PushURL    string
		Redirect   string
		Refresh    bool
		ReplaceURL string
		Trigger    string
		NoContent  bool
	}
)

// GetRequest reads the htmx headers of r. A request that did not come from htmx has none of them, and gives the
// zero Request.
func GetRequest(r *http.Request) Request {
	return Request{
		Enabled:     r.Header.Get(HeaderRequest) == "true",
		Boosted:     r.Header.Get(HeaderBoosted) == "true",
		Source:      r.Header.Get(HeaderSource),
		Target:      r.Header.Get(HeaderTarget),
		RequestType: r.Header.Get(HeaderRequestType),
	}
}

// Apply sets the headers of the response, and, for NoContent, answers 204 on the spot: it is the last thing a
// handler does with it.
func (r Response) Apply(w http.ResponseWriter) {
	h := w.Header()

	if r.PushURL != "" {
		h.Set(HeaderPushURL, r.PushURL)
	}
	if r.Redirect != "" {
		h.Set(HeaderRedirect, r.Redirect)
	}
	if r.Refresh {
		h.Set(HeaderRefresh, "true")
	}
	if r.Trigger != "" {
		h.Set(HeaderTrigger, r.Trigger)
	}
	if r.ReplaceURL != "" {
		h.Set(HeaderReplaceURL, r.ReplaceURL)
	}
	if r.NoContent {
		w.WriteHeader(http.StatusNoContent)
	}
}
