// Package domain is what the auth domain knows without a database or a request: how a login is judged, the hash
// of a password and the random token. It imports no HTTP, JSON or database package, and neither the business root
// (internal/auth) nor the adapters, so that cmd/admin can use it without the site.
package domain

import (
	"errors"

	"github.com/tung-dnt/lean-go/pkg/form"
)

// ErrInvalidCredentials is what a login answers for an unknown email and for a wrong password alike, so that
// nothing tells a caller which of the two it was.
var ErrInvalidCredentials = errors.New("invalid credentials")

// CredentialProblems says what is wrong with the email and the password of a login, each "" when nothing is: both
// have to be filled in, and the first has to be an address.
func CredentialProblems(email, password string) (emailProblem, passwordProblem string) {
	switch {
	case email == "":
		emailProblem = form.MsgRequired
	case !form.IsEmail(email):
		emailProblem = form.MsgEmail
	}
	if password == "" {
		passwordProblem = form.MsgRequired
	}

	return emailProblem, passwordProblem
}
