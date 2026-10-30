package layouts

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/components"
	"github.com/tung-dnt/lean-go/internal/ui/icons"
)

// Primary is the shell of the pages of the site: the content beside a sidebar with the menu.
func Primary(r *ui.Request, content g.Node) g.Node {
	return document(r,
		h.Div(
			h.Class("drawer lg:drawer-open"),
			h.Input(
				h.ID("sidebar"),
				h.Type("checkbox"),
				h.Class("drawer-toggle"),
			),
			h.Div(
				h.Class("drawer-content flex flex-col p-7 prose-base [view-transition-name:page-content]"),
				g.If(len(r.Title) > 0, h.H1(g.Text(r.Title))),
				components.FlashMessages(r),
				content,
				h.Label(
					h.For("sidebar"),
					h.Class("btn btn-primary drawer-button lg:hidden"),
					g.Text("Open drawer"),
				),
			),
			sidebarMenu(r),
		),
	)
}

// sidebarMenu links only to registered routes: an unregistered route name reverses to an empty
// href, which would silently reload the current page.
func sidebarMenu(r *ui.Request) g.Node {
	header := func(text string) g.Node {
		return h.Li(
			h.Class("menu-title mt-3 uppercase"),
			h.Span(g.Text(text)),
		)
	}

	return h.Div(
		h.Class("drawer-side"),
		h.Label(
			h.For("sidebar"),
			h.Aria("label", "close sidebar"),
			h.Class("drawer-overlay"),
		),
		h.Div(
			h.Class("menu bg-base-200 text-base-content min-h-full w-80 p-4"),
			h.Div(
				h.Class("w-2/3 mx-auto mt-3 mb-10"),
				h.Img(
					h.Src(ui.StaticFile("logo.png")),
				),
			),
			h.Ul(
				components.HxBoost(),
				// Land boosted navigations at the top of the page instead of
				// keeping the previous page's scroll position.
				g.Attr("hx-swap:inherited", "innerHTML show:window:top"),
				// Only safe navigation destinations preload; GET /logout must not.
				g.Attr("hx-preload:inherited", "mouseover"),
				header("General"),
				components.MenuLink(r, icons.Home(), "Notes", paths.Home),
				g.If(!r.IsAuth, components.MenuLink(r, icons.Enter(), "Log in", paths.Login)),
			),
			// Only a logged-in account has any use for this, so it is not even built for anyone else. It is not
			// warmed on hover like the links above: a GET of /logout logs out.
			g.Iff(r.IsAuth, func() g.Node {
				return h.Ul(
					components.HxBoost(),
					g.Attr("hx-swap:inherited", "innerHTML show:window:top"),
					header("Account"),
					components.MenuLink(r, icons.Exit(), "Log out", paths.Logout),
				)
			}),
		),
	)
}
