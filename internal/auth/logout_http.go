package auth

import (
	"errors"
	"net/http"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/redirect"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// HandleLogout logs the user out (GET /logout): the session is emptied, and the visitor is sent home with a message.
func HandleLogout() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sess, ok := session.From(r.Context())
		if !ok {
			layouts.Error(w, r, errors.New("unable to log out user: the request has no session"))

			return
		}
		sess.Clear()

		msg.Success(r.Context(), "You have been logged out successfully.")
		redirect.To(w, r, paths.Home, http.StatusTemporaryRedirect)
	})
}
