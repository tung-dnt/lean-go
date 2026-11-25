// Package adapters is the presentation of the auth domain: the login page and the logout, with the session, the
// flash message and the redirect that go with them. A handler reads the request, calls the business root
// (internal/auth) with plain parameters, and answers with the pages of internal/ui.
package adapters

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/tung-dnt/lean-go/internal/auth"
	"github.com/tung-dnt/lean-go/internal/auth/domain"
	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/redirect"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// HandleLogin is the page of the login form (GET /login). It needs nothing: an empty form is drawn from nothing.
func HandleLogin() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		renderLogin(w, r, &loginForm{})
	})
}

// HandleLoginSubmit logs the user in (POST /login). A form that is not filled in right, and an email and a
// password that are not an account's, are answered with the form again; the second with one message for the
// unknown email and the wrong password alike.
func HandleLoginSubmit(q *db.Queries) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in loginForm
		if err := form.Decode(r, &in); err != nil {
			layouts.Error(w, r, err)

			return
		}
		if !in.IsValid() {
			renderLogin(w, r, &in)

			return
		}

		// Check the credentials: the same answer for an unknown email and for a wrong password.
		u, err := auth.Authenticate(r.Context(), q, in.Email, in.Password)
		switch {
		case errors.Is(err, domain.ErrInvalidCredentials):
			in.SetFieldError("Email", "")
			in.SetFieldError("Password", "")
			msg.Error(r.Context(), "Invalid credentials. Please try again.")
			renderLogin(w, r, &in)

			return
		case err != nil:
			layouts.Error(w, r, fmt.Errorf("error querying user during login: %w", err))

			return
		}

		// Log the user in.
		sess, ok := session.From(r.Context())
		if !ok {
			layouts.Error(w, r, errors.New("unable to log in user: the request has no session"))

			return
		}
		sess.Clear()
		sess.SetUserID(u.ID)

		msg.Success(r.Context(), fmt.Sprintf("Welcome back, %s. You are now logged in.", u.Name))

		// See Other, so that a form posted without htmx is answered with a GET of the page. The default, a 307,
		// would post the password again to the home page.
		redirect.To(w, r, paths.Home, http.StatusSeeOther)
	})
}
