// Package ui is the page shell: the Request that a page is drawn from, built from the HTTP request, and Render, which
// writes a page in a layout. The building blocks of a page are in ui/components, the layouts, and the error
// page, in ui/layouts, the icons in ui/icons, and ui/cache keeps static nodes that are rendered once.
package ui

import (
	"bytes"
	"context"
	"fmt"
	"net/http"

	"maragu.dev/gomponents"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/pkg/config"
	"github.com/tung-dnt/lean-go/pkg/htmx"
	"github.com/tung-dnt/lean-go/pkg/middleware"
)

type (
	// Request encapsulates information about the incoming request in order to provide your ui with important and
	// useful information needed for rendering.
	Request struct {
		// Title stores the title of the page.
		Title string

		// Context stores the context of the request, which is where the flash messages are read from.
		Context context.Context

		// CurrentPath stores the path of the current request.
		CurrentPath string

		// IsHome stores whether the requested page is the home page.
		IsHome bool

		// IsAuth stores whether the user is authenticated.
		IsAuth bool

		// IsAdmin stores whether the user is an admin.
		IsAdmin bool

		// AuthUser stores the authenticated user.
		AuthUser *db.User

		// Metatags stores metatag values.
		Metatags struct {
			// Description stores the description metatag value.
			Description string

			// Keywords stores the keywords metatag values.
			Keywords []string
		}

		// Htmx stores information provided by HTMX about this request.
		Htmx htmx.Request

		// Config stores the application configuration, as the server's middleware (middleware.WithConfig) put it
		// in the context of the request. It is never nil: a request that did not go through that middleware has
		// the empty configuration, so that drawing a page, an error page included, cannot fail for the lack of it.
		Config *config.Config
	}

	// LayoutFunc is a callback function intended to render your page node within a given layout.
	// This is handled as a callback to automatically support HTMX requests so that you can respond
	// with only the page content and not the entire layout.
	// See Render.
	LayoutFunc func(*Request, gomponents.Node) gomponents.Node
)

// NewRequest generates a new Request for the HTTP request of a handler.
func NewRequest(r *http.Request) *Request {
	p := &Request{
		Context:     r.Context(),
		CurrentPath: r.URL.Path,
		Htmx:        htmx.GetRequest(r),
		Config:      config.FromContext(r.Context()),
	}

	p.IsHome = p.CurrentPath == "/"

	if u, ok := middleware.User(r.Context()); ok {
		p.IsAuth = true
		p.AuthUser = u
		p.IsAdmin = u.Admin
	}

	if p.Config == nil {
		p.Config = &config.Config{}
	}

	return p
}

// URL generates an absolute URL for a path of the site (take it from pkg/paths), which is what the
// configuration says the site's address is.
func (r *Request) URL(path string) string {
	return r.Config.App.Host + path
}

// Render writes the node of a page to w, in the layout, as HTML, with a status of 200. See RenderStatus.
func Render(w http.ResponseWriter, r *Request, layout LayoutFunc, node gomponents.Node) error {
	return RenderStatus(w, http.StatusOK, r, layout, node)
}

// RenderStatus writes the node of a page to w, in the layout, as HTML, with the status. If the request is being
// made by HTMX and is not boosted, this will automatically only render the node without the layout, to support
// partial rendering.
//
// The page is built and rendered before any of it is written, and only then are the status and the headers sent.
// That matters twice. A page that cannot be built is an error that the caller can turn into the error page
// (layouts.Error), and not half a page with a status on it. And what building the page does to the session, which
// is where the flash messages are taken out of (components.FlashMessages), is done before the headers go out and
// so is saved: a handler that called w.WriteHeader itself before it rendered the page would show the messages
// again on the next one. Send a status other than 200 with RenderStatus, and do not call WriteHeader first.
func RenderStatus(w http.ResponseWriter, status int, r *Request, layout LayoutFunc, node gomponents.Node) error {
	if !r.Htmx.Enabled || r.Htmx.Boosted {
		node = layout(r, node)
	}

	var page bytes.Buffer
	if err := node.Render(&page); err != nil {
		return fmt.Errorf("render the page: %w", err)
	}

	if w.Header().Get("Content-Type") == "" {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
	}
	w.WriteHeader(status)
	if _, err := w.Write(page.Bytes()); err != nil {
		return fmt.Errorf("write the page: %w", err)
	}

	return nil
}
