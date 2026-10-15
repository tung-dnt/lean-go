package middleware

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/log"
	"github.com/tung-dnt/lean-go/pkg/redirect"
	"github.com/tung-dnt/lean-go/pkg/session"
)

// userKey is the key of the authenticated user in a context.
type userKey struct{}

// WithUser returns a copy of ctx that carries the authenticated user, for User. LoadUser does it for the user of
// the session; it is public so that a test can make a context with a user in it.
func WithUser(ctx context.Context, u *db.User) context.Context {
	return context.WithValue(ctx, userKey{}, u)
}

// User is the user that the request is authenticated as, and false for a visitor who is not logged in.
func User(ctx context.Context) (*db.User, bool) {
	u, ok := ctx.Value(userKey{}).(*db.User)

	return u, ok && u != nil
}

// LoadUser finds the user the session says is logged in, and puts them in the context of the request (User). It
// goes after the session middleware, and before every guard; the domains never import auth for it.
//
// A visitor with no user in the session is not an error, and goes on as anyone who is not logged in. So does a
// session that names a user who is not there any more (an account that was removed): it is logged, and the
// session forgets them, so that the next request does not ask again. A query that fails is a 500.
func LoadUser(q *db.Queries, fail ErrorFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			sess, ok := session.From(r.Context())
			if !ok {
				next.ServeHTTP(w, r)

				return
			}
			id, ok := sess.UserID()
			if !ok {
				next.ServeHTTP(w, r)

				return
			}

			u, err := q.GetUser(r.Context(), id)
			switch {
			case err == nil:
				r = r.WithContext(WithUser(r.Context(), &u))
			case errors.Is(err, pgx.ErrNoRows):
				log.Ctx(r.Context()).Warn("auth user not found", "user_id", id)
				sess.ClearUserID()
			default:
				fail(w, r, fmt.Errorf("error querying for authenticated user: %w", err))

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAuth requires that the user be logged in in order to proceed: anyone else is refused with a 401.
func RequireAuth(fail ErrorFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := User(r.Context()); !ok {
				fail(w, r, httperr.New(http.StatusUnauthorized, "login required"))

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireNoAuth requires that the user not be logged in in order to proceed, as the login page does: whoever is
// logged in is refused with a 403.
func RequireNoAuth(fail ErrorFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if _, ok := User(r.Context()); ok {
				fail(w, r, httperr.New(http.StatusForbidden, "already logged in"))

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}

// RequireAdmin requires that the logged in user be an admin in order to proceed.
//
// A visitor who is not logged in is sent to loginPath (a 302, or an HX-Redirect to a boosted request), since
// logging in is how they would become the admin: a bare 401 would leave them at a dead end. The redirect carries
// nothing but that address, and is the same for a path that exists and one that does not, so nothing behind it is
// served to them. Someone who is logged in but is not an admin is refused with a 403 instead: logging in again
// would not change who they are.
func RequireAdmin(loginPath string, fail ErrorFunc) Middleware {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			u, ok := User(r.Context())
			if !ok {
				redirect.To(w, r, loginPath, http.StatusFound)

				return
			}

			if !u.Admin {
				fail(w, r, httperr.New(http.StatusForbidden, "admin only"))

				return
			}

			next.ServeHTTP(w, r)
		})
	}
}
