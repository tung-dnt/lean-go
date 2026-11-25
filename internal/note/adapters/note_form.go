package adapters

import (
	"context"

	"github.com/tung-dnt/lean-go/internal/note/domain"
	"github.com/tung-dnt/lean-go/pkg/form"
)

// noteForm is the form of a new note, which is also what a post of it is read into.
type noteForm struct {
	Title string `form:"title"`
	Body  string `form:"body"`
	form.Submission
}

// Valid implements form.Validator: the rules are the domain's, and what is wrong is shown under the field it is
// about.
func (f *noteForm) Valid(context.Context) map[string]string {
	problems := map[string]string{}

	if problem := domain.TitleProblem(f.Title); problem != "" {
		problems["Title"] = problem
	}
	if problem := domain.BodyProblem(f.Body); problem != "" {
		problems["Body"] = problem
	}

	return problems
}
