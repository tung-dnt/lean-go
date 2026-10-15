// Package htmx reads what htmx says about the request it sent, and says to htmx what to do with the response.
package htmx

import (
	"net/http"
)

// Request headers: https://htmx.org/docs/#request-headers
const (
	HeaderBoosted               = "HX-Boosted"
	HeaderHistoryRestoreRequest = "HX-History-Restore-Request"
	HeaderPrompt                = "HX-Prompt"
	HeaderRequest               = "HX-Request"
	HeaderTarget                = "HX-Target"
	HeaderTrigger               = "HX-Trigger"
	HeaderTriggerName           = "HX-Trigger-Name"
)

// Response headers: https://htmx.org/docs/#response-headers
const (
	HeaderPushURL            = "HX-Push-Url"
	HeaderRedirect           = "HX-Redirect"
	HeaderReplaceURL         = "HX-Replace-Url"
	HeaderRefresh            = "HX-Refresh"
	HeaderTriggerAfterSettle = "HX-Trigger-After-Settle"
	HeaderTriggerAfterSwap   = "HX-Trigger-After-Swap"
)

type (
	// Request contains data that HTMX provides during requests.
	Request struct {
		Enabled        bool
		Boosted        bool
		HistoryRestore bool
		Trigger        string
		TriggerName    string
		Target         string
		Prompt         string
	}

	// Response contain data that the server can communicate back to HTMX.
	Response struct {
		PushURL            string
		Redirect           string
		Refresh            bool
		ReplaceURL         string
		Trigger            string
		TriggerAfterSwap   string
		TriggerAfterSettle string
		NoContent          bool
	}
)

// GetRequest reads the htmx headers of r. A request that did not come from htmx has none of them, and gives the
// zero Request.
func GetRequest(r *http.Request) Request {
	return Request{
		Enabled:        r.Header.Get(HeaderRequest) == "true",
		Boosted:        r.Header.Get(HeaderBoosted) == "true",
		Trigger:        r.Header.Get(HeaderTrigger),
		TriggerName:    r.Header.Get(HeaderTriggerName),
		Target:         r.Header.Get(HeaderTarget),
		Prompt:         r.Header.Get(HeaderPrompt),
		HistoryRestore: r.Header.Get(HeaderHistoryRestoreRequest) == "true",
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
	if r.TriggerAfterSwap != "" {
		h.Set(HeaderTriggerAfterSwap, r.TriggerAfterSwap)
	}
	if r.TriggerAfterSettle != "" {
		h.Set(HeaderTriggerAfterSettle, r.TriggerAfterSettle)
	}
	if r.ReplaceURL != "" {
		h.Set(HeaderReplaceURL, r.ReplaceURL)
	}
	if r.NoContent {
		w.WriteHeader(http.StatusNoContent)
	}
}
