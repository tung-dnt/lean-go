package auth

import (
	"context"
	"net/http"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/components"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/form"
)

// loginForm is the form of the login page, which is also what a post of it is read into.
type loginForm struct {
	Email    string `form:"email"`
	Password string `form:"password"`
	form.Submission
}

// Valid implements form.Validator: both fields have to be filled in, and the first has to be an address.
func (f *loginForm) Valid(context.Context) map[string]string {
	problems := map[string]string{}

	switch {
	case f.Email == "":
		problems["Email"] = form.MsgRequired
	case !form.IsEmail(f.Email):
		problems["Email"] = form.MsgEmail
	}
	if f.Password == "" {
		problems["Password"] = form.MsgRequired
	}

	return problems
}

// renderLogin draws the login page for the form, which is empty for the first visit and has what was entered and
// what is wrong with it after a post. The password is never sent back.
func renderLogin(w http.ResponseWriter, r *http.Request, f *loginForm) {
	page := ui.NewRequest(r)
	page.Title = "Login"

	if err := ui.Render(w, page, layouts.Auth, f.render(page)); err != nil {
		layouts.Error(w, r, err)
	}
}

// render is the form: the email and password inputs, and the buttons to log in or go back.
func (f *loginForm) render(r *ui.Request) g.Node {
	return h.Form(
		h.ID("login"),
		h.Method(http.MethodPost),
		components.HxBoost(),
		h.Action(paths.Login),
		components.FlashMessages(r),
		components.InputField(components.InputFieldParams{
			Form:      f,
			FormField: "Email",
			Name:      "email",
			InputType: "email",
			Label:     "Email address",
			Value:     f.Email,
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
