// Package pages is every full page of the site: a URL that a browser navigates to, drawn in a layout. One file per
// page, named by the URL path (segments joined by "_", {x} written x, "/" is home) and ending in _page.go. A page is
// a template: it takes what it draws as plain parameters and imports no domain and nothing of internal/external;
// the handler of the domain loads, maps and calls it, and chooses the layout. Fragments and answers to a post live
// in the handler's file of the domain, and markup that a page and a fragment both draw is in internal/ui/components.
package pages

import (
	"net/http"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/components"
	"github.com/tung-dnt/lean-go/pkg/form"
)

// LoginPage is the login form (GET /login, and the answer to a post of it that is not a login): the email and
// password inputs, and the buttons to log in or go back. f holds what is wrong with the post, shown under each
// field, and email is what was entered; the password is never sent back. The handler draws it in layouts.Auth.
func LoginPage(page *ui.Request, f form.Form, email string) g.Node {
	page.Title = "Login"

	return h.Form(
		h.ID("login"),
		h.Method(http.MethodPost),
		components.HxBoost(),
		h.Action(paths.Login),
		components.FlashMessages(page),
		components.InputField(components.InputFieldParams{
			Form:      f,
			FormField: "Email",
			Name:      "email",
			InputType: "email",
			Label:     "Email address",
			Value:     email,
		}),
		components.InputField(components.InputFieldParams{
			Form:        f,
			FormField:   "Password",
			Name:        "password",
			InputType:   "password",
			Label:       "Password",
			Placeholder: "******",
		}),
		components.ControlGroup(
			components.FormButton(components.ColorPrimary, "Login"),
			components.ButtonLink(components.ColorLink, paths.Home, "Cancel"),
		),
	)
}
