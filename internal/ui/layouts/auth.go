// Package layouts holds the page shells, Primary and Auth, that wrap the content of a page, and Error, the one
// error page.
package layouts

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/components"
)

// Auth is the shell of the pages of a visitor who is not signed in: a card in the middle of the screen, with the
// title of the request and the flash messages above the content.
func Auth(r *ui.Request, content g.Node) g.Node {
	return document(r,
		h.Div(
			h.Class("hero flex items-center justify-center min-h-screen"),
			h.Div(
				h.Class("flex-col hero-content"),
				h.Div(
					h.Class("card shadow-md bg-base-200 w-96"),
					h.Div(
						h.Class("card-body"),
						g.If(len(r.Title) > 0, h.H1(h.Class("text-2xl font-bold"), g.Text(r.Title))),
						components.FlashMessages(r),
						content,
					),
				),
			),
		),
	)
}

// document is the HTML document every page shell draws: the head (meta tags, CSS, JS) and a body that holds body.
func document(r *ui.Request, body ...g.Node) g.Node {
	return h.Doctype(
		h.HTML(
			h.Lang("en"),
			h.Data("theme", "dark"),
			h.Head(
				components.Metatags(r),
				components.CSS(),
				components.JS(),
			),
			h.Body(body...),
		),
	)
}
