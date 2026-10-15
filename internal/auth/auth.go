// Package auth is how the admin logs in and out. The site has exactly one account, made by cmd/admin, so there is
// no registration, no password reset and no email verification: none of them has a handler, and an unlinked route
// would still be routable. The login page and the logout page are the whole of it.
//
// Who is logged in is kept in the session (pkg/session): the login puts the id of the user there, the
// logout takes everything out. Finding the user again on the next request is not this package's work. It is the
// server's middleware (middleware.LoadUser), which every page goes through, so that no domain has to import auth
// to know who is asking, and the guards (middleware.RequireAuth, RequireNoAuth, RequireAdmin) are applied to the
// routes in internal/server/routes.go.
//
// auth is a leaf domain: it imports no other.
package auth

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/paths"
	"github.com/tung-dnt/lean-go/pkg/redirect"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// ErrInvalidCredentials is what authenticate returns for an unknown email and for a wrong password alike, so
// that nothing tells a caller which of the two it was.
var ErrInvalidCredentials = errors.New("invalid credentials")

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
		u, err := authenticate(r.Context(), q, in.Email, in.Password)
		switch {
		case errors.Is(err, ErrInvalidCredentials):
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

// authenticate returns the user of an email whose password matches, or ErrInvalidCredentials when no user has
// that email or the password is wrong. Any other failure is returned wrapped, and carries neither the email nor
// the password.
func authenticate(ctx context.Context, q *db.Queries, email, password string) (*db.User, error) {
	u, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrInvalidCredentials
		}

		return nil, fmt.Errorf("get user by email: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)); err != nil {
		return nil, ErrInvalidCredentials
	}

	return &u, nil
}

// HashPassword hashes a plain-text password so that it can be stored in the database.
// This must be called before persisting any password; nothing in the database layer hashes for you.
func HashPassword(password string) (string, error) {
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", err
	}

	return string(hash), nil
}

// RandomToken generates a random token string of a given length
func RandomToken(length int) (string, error) {
	b := make([]byte, (length/2)+1)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}

	return hex.EncodeToString(b)[:length], nil
}
