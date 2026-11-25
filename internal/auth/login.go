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
// A domain is three packages. This one is its business, the check of a login (Authenticate), which calls the
// generated sqlc queries directly and answers with what auth/domain defines. auth/domain holds the rules, and
// auth/adapters the session, the form and the pages that internal/server routes to.
//
// auth is a leaf domain: it imports no other.
package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/tung-dnt/lean-go/internal/auth/domain"
	"github.com/tung-dnt/lean-go/internal/external/db"
)

// Authenticate returns the user of an email whose password matches, or domain.ErrInvalidCredentials when no user
// has that email or the password is wrong. Any other failure is returned wrapped, and carries neither the email
// nor the password.
func Authenticate(ctx context.Context, q *db.Queries, email, password string) (*db.User, error) {
	u, err := q.GetUserByEmail(ctx, email)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domain.ErrInvalidCredentials
		}

		return nil, fmt.Errorf("get user by email: %w", err)
	}

	if err := bcrypt.CompareHashAndPassword([]byte(u.Password), []byte(password)); err != nil {
		return nil, domain.ErrInvalidCredentials
	}

	return &u, nil
}
