package pages

import (
	"net/http"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/components"
)

// LogoutPage is the confirmation page of a log out (GET /logout): a form that posts to the same path, where the
// session is ended. Opening or preloading the page changes nothing; only the post does. The handler draws it in
// layouts.Primary through layouts.Page.
func LogoutPage(page *ui.Request) g.Node {
	page.Title = "Log out"

	return h.Form(
		h.ID("logout"),
		h.Method(http.MethodPost),
		components.HxBoost(),
		h.Action(paths.Logout),
		h.P(g.Text("Log out of this account?")),
		components.ControlGroup(
			components.FormButton(components.ColorPrimary, "Log out"),
			components.ButtonLink(components.ColorLink, paths.Home, "Cancel"),
		),
	)
}
