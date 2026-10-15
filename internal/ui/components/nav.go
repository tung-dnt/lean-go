package components

import (
	"fmt"
	"strings"

	g "maragu.dev/gomponents"
	gc "maragu.dev/gomponents/components"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/pkg/pager"
)

// MenuLink is an item of a menu that links to href, a path of the site (internal/server/paths), and is marked as the
// current one when it is the path of the request.
func MenuLink(r *ui.Request, icon g.Node, title, href string) g.Node {

	return h.Li(
		h.Class("ml-2"),
		h.A(
			h.Href(href),
			icon,
			g.Text(title),
			gc.Classes{
				"menu-active": href == r.CurrentPath,
				"p-2":         true,
			},
		),
	)
}

// Pager links to the previous and next page. path may already carry a query string, ie: a search's
// "/search?q=cat", in which case the page parameter is appended to it rather than starting a new one.
func Pager(page int, path string, hasNext bool, hxTarget string) g.Node {
	sep := "?"
	if strings.Contains(path, "?") {
		sep = "&"
	}
	href := func(page int) string {
		return fmt.Sprintf("%s%s%s=%d",
			path,
			sep,
			pager.QueryKey,
			page,
		)
	}

	return h.Div(
		h.Class("join"),
		h.A(
			h.Class("join-item btn"),
			g.Text("«"),
			g.If(page <= 1, h.Disabled()),
			h.Href(href(page-1)),
			g.Iff(len(hxTarget) > 0, func() g.Node {
				return g.Group{
					g.Attr("hx-get", href(page-1)),
					g.Attr("hx-swap", "outerHTML"),
					g.Attr("hx-target", hxTarget),
				}
			}),
		),
		h.Button(
			h.Class("join-item btn"),
			g.Textf("Page %d", page),
		),
		h.A(
			h.Class("join-item btn"),
			g.Text("»"),
			g.If(!hasNext, h.Disabled()),
			h.Href(href(page+1)),
			g.Iff(len(hxTarget) > 0, func() g.Node {
				return g.Group{
					g.Attr("hx-get", href(page+1)),
					g.Attr("hx-swap", "outerHTML"),
					g.Attr("hx-target", hxTarget),
				}
			}),
		),
	)
}
