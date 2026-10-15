package components

import (
	"strings"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/ui"
)

// JS is the script of the site, the bundle that web/js/app.ts builds.
func JS() g.Node {
	return g.Group{
		// An ES module so esbuild's split chunks load via import(); modules are deferred by default.
		h.Script(h.Type("module"), h.Src(ui.StaticFile("js/app.js"))),
	}
}

// CSS is the stylesheet of the site.
func CSS() g.Node {
	return h.Link(
		h.Href(ui.StaticFile("main.css")),
		h.Rel("stylesheet"),
		h.Type("text/css"),
	)
}

// Metatags renders the head of a page that does not depend on the page: the charset, viewport and icon, the title
// from the request, and the description and keywords when it has them.
func Metatags(r *ui.Request) g.Node {
	return g.Group{
		h.Meta(h.Charset("utf-8")),
		h.Meta(h.Name("viewport"), h.Content("width=device-width, initial-scale=1")),
		h.Link(h.Rel("icon"), h.Href(ui.StaticFile("favicon.png"))),
		h.TitleEl(g.Text(r.Config.App.Name), g.If(r.Title != "", g.Text(" | "+r.Title))),
		g.If(r.Metatags.Description != "", h.Meta(h.Name("description"), h.Content(r.Metatags.Description))),
		g.If(len(r.Metatags.Keywords) > 0, h.Meta(h.Name("keywords"), h.Content(strings.Join(r.Metatags.Keywords, ", ")))),
	}
}
