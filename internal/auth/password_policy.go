package auth

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"
	"golang.org/x/crypto/bcrypt"

	"github.com/tung-dnt/lean-go/internal/external/db"
)

// ErrInvalidCredentials is what authenticate returns for an unknown email and for a wrong password alike, so
// that nothing tells a caller which of the two it was.
var ErrInvalidCredentials = errors.New("invalid credentials")

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
