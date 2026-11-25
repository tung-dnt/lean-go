package adapters

import (
	"context"

	"github.com/tung-dnt/lean-go/internal/auth/domain"
	"github.com/tung-dnt/lean-go/pkg/form"
)

// loginForm is the form of the login page, which is also what a post of it is read into.
type loginForm struct {
	Email    string `form:"email"`
	Password string `form:"password"`
	form.Submission
}

// Valid implements form.Validator: the rules are the domain's, and what is wrong is shown under the field it is
// about.
func (f *loginForm) Valid(context.Context) map[string]string {
	problems := map[string]string{}

	emailProblem, passwordProblem := domain.CredentialProblems(f.Email, f.Password)
	if emailProblem != "" {
		problems["Email"] = emailProblem
	}
	if passwordProblem != "" {
		problems["Password"] = passwordProblem
	}

	return problems
}
