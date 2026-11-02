package auth

import (
	"errors"
	"net/http"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/internal/ui/pages"
	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/redirect"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// HandleLogout is the confirmation page of a log out (GET /logout). It changes nothing: a link, a preload or a
// crawler that opens it leaves the session as it was. HandleLogoutSubmit is the post of its form.
func HandleLogout() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		page := ui.NewRequest(r)
		layouts.Page(w, r, page, pages.LogoutPage(page))
	})
}

// HandleLogoutSubmit logs the user out (POST /logout): the session is emptied, and the visitor is sent home with a
// message. See Other, so that a form posted without htmx is answered with a GET of the home page: the default, a
// 307, would post to it again.
func HandleLogoutSubmit() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := session.From(r.Context())
		if !ok {
			layouts.Error(w, r, errors.New("unable to log out user: the request has no session"))

			return
		}
		sess.Clear()

		msg.Success(r.Context(), "You have been logged out successfully.")
		redirect.To(w, r, paths.Home, http.StatusSeeOther)
	})
}
