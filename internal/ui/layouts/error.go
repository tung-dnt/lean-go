package layouts

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/log"
	"github.com/tung-dnt/lean-go/pkg/middleware"
	"github.com/tung-dnt/lean-go/pkg/paths"
)

// Error is the one place a failed request is answered: every handler and every middleware that cannot serve a
// request ends in it, and the visitor gets the error page, in the layout of the site, with the status the error
// stands for.
//
// What the error is decides the answer:
//
//   - an *httperr.Error is the status it carries;
//   - a *http.MaxBytesError, a body over the limit of http.MaxBytesReader, is a 413;
//   - a request that ran out of time (the cause of its context is httperr.ErrTimeout, or the error is
//     context.DeadlineExceeded) is a 503;
//   - a request that was cancelled for any other reason is a client that went away, and there is nobody to answer:
//     nothing is written;
//   - anything else is a 500.
//
// The error is logged, as an error for a 5xx and as a warning for a 4xx, with the logger of the request. The text
// of the error is never shown: of an *httperr.Error only the message of a 413 is, since that is the one page that
// has to say what the limit is; every other page says the same to anyone. A response that has been started
// cannot be turned into the error page, so the error is logged and nothing more is written.
//
// It has the signature of middleware.ErrorFunc, which is how the middleware of pkg, that cannot
// import this package, answer a request that they refuse.
func Error(w http.ResponseWriter, r *http.Request, err error) {
	status, tooLarge := classify(r.Context(), err)
	if status == 0 {
		log.Ctx(r.Context()).Debug("the request was cancelled", "error", err)

		return
	}

	logger := log.Ctx(r.Context())
	switch {
	case status >= http.StatusInternalServerError:
		logger.Error(err.Error())
	case status >= http.StatusBadRequest:
		logger.Warn(err.Error())
	}

	if middleware.Written(w) {
		logger.Error("the error came after the response was started, so there is no error page", "status", status)

		return
	}

	page := ui.NewRequest(r)
	page.Title = http.StatusText(status)
	if err := ui.RenderStatus(w, status, page, Primary, h.P(errorText(status, tooLarge))); err != nil {
		logger.Error("failed to render error page", "error", err)
	}
}

// classify is the status that an error is answered with, and the sentence that a 413 says. The status is 0 for a
// request that nobody waits for any more.
func classify(ctx context.Context, err error) (status int, tooLarge string) {
	var he *httperr.Error
	var tooBig *http.MaxBytesError

	switch {
	case errors.As(err, &he):
		return he.Status, he.Message
	case errors.As(err, &tooBig):
		return http.StatusRequestEntityTooLarge, fmt.Sprintf("That request is too large. It can be up to %d MB.", tooBig.Limit>>20)
	case errors.Is(context.Cause(ctx), httperr.ErrTimeout), errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable, ""
	case errors.Is(err, context.Canceled):
		return 0, ""
	}

	return http.StatusInternalServerError, ""
}

// errorText is what the page of an error status says.
func errorText(status int, tooLarge string) g.Node {
	switch status {
	case http.StatusInternalServerError:
		return g.Text("Please try again.")
	case http.StatusForbidden, http.StatusUnauthorized:
		return g.Text("You are not authorized to view the requested page.")
	case http.StatusRequestEntityTooLarge:
		if tooLarge == "" {
			tooLarge = "That request is too large."
		}

		return g.Text(tooLarge)
	case http.StatusNotFound:
		return g.Group{
			g.Text("Click "),
			h.A(
				h.Href(paths.Home),
				g.Text("here"),
			),
			g.Text(" to go return home."),
		}
	}

	return g.Text("Something went wrong.")
}
