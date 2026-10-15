// Package form reads the body of a form post into a struct (Decode) and keeps what a view needs to draw the form
// again when it is not accepted: that it was submitted, and what is wrong with each field.
//
// A form is a struct. Its fields carry the name of the input in a `form` tag, it embeds Submission, and it checks
// itself with a Valid method, which is what replaced validator tags:
//
//	type loginForm struct {
//		Email    string `form:"email"`
//		Password string `form:"password"`
//		form.Submission
//	}
//
//	func (f *loginForm) Valid(ctx context.Context) map[string]string {
//		problems := map[string]string{}
//		if !form.IsEmail(f.Email) {
//			problems["Email"] = form.MsgEmail
//		}
//		if f.Password == "" {
//			problems["Password"] = form.MsgRequired
//		}
//		return problems
//	}
//
// A handler calls Decode and then asks the form whether it is valid. The keys of the problems are the names of
// the Go fields, "Email" and not "email", because that is what the form components of internal/ui look them up by.
package form

import (
	"context"
	"net/mail"
)

// Validator is a form that can say what is wrong with it. Valid returns the problems by field name, each with the
// sentence to show under the field, and an empty map when the form is acceptable. Decode calls it on a form that
// has the method.
type Validator interface {
	Valid(ctx context.Context) map[string]string
}

// The sentences of the checks that forms make most often.
const (
	// MsgRequired is the problem of a field that was left empty.
	MsgRequired = "This field is required."

	// MsgEmail is the problem of an address that is not one.
	MsgEmail = "Enter a valid email address."
)

// IsEmail reports whether s is an email address: a bare address, "a@b.co", and not a name with an address in
// angle brackets, which is what net/mail would take it for.
func IsEmail(s string) bool {
	addr, err := mail.ParseAddress(s)

	return err == nil && addr.Address == s
}

// Form is what a view needs to know of a form to draw it: whether it was submitted, and the problems with its
// fields. The form components of internal/ui take one.
type Form interface {
	// IsSubmitted returns true if the form was submitted.
	IsSubmitted() bool

	// IsValid returns true if the form has no validation errors.
	IsValid() bool

	// FieldHasErrors returns true if a given struct field has validation errors.
	FieldHasErrors(fieldName string) bool

	// SetFieldError sets a validation error message for a given struct field.
	SetFieldError(fieldName string, message string)

	// GetFieldErrors returns the validation errors for a given struct field.
	GetFieldErrors(fieldName string) []string
}

// Submission is the state of a form that was submitted, apart from the form itself. Embed it in a form struct;
// it satisfies Form. The zero value is a form that was not submitted yet, which is what the page of an empty form
// is drawn from.
type Submission struct {
	// submitted indicates if the form has been submitted.
	submitted bool

	// errors stores a slice of error message strings keyed by form struct field name.
	errors map[string][]string
}

// markSubmitted records that the form came from a request. Decode does.
func (f *Submission) markSubmitted() {
	f.submitted = true
}

// IsSubmitted implements Form.
func (f *Submission) IsSubmitted() bool {
	return f.submitted
}

// IsValid implements Form. A form with an error on any field is not valid, a message left empty included: it
// marks the field and says nothing.
func (f *Submission) IsValid() bool {
	return len(f.errors) == 0
}

// FieldHasErrors implements Form.
func (f *Submission) FieldHasErrors(fieldName string) bool {
	return len(f.GetFieldErrors(fieldName)) > 0
}

// SetFieldError implements Form. The handler uses it for what only it can know, that the invite code was not the
// right one, say; what the form can check of itself is the work of Valid.
func (f *Submission) SetFieldError(fieldName string, message string) {
	if f.errors == nil {
		f.errors = make(map[string][]string)
	}
	f.errors[fieldName] = append(f.errors[fieldName], message)
}

// GetFieldErrors implements Form.
func (f *Submission) GetFieldErrors(fieldName string) []string {
	if f.errors == nil {
		return []string{}
	}
	return f.errors[fieldName]
}
