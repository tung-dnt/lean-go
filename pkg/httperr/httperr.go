// Package httperr is how a failure says which HTTP status it is. A handler that has to refuse a request returns
// to the error page (layouts.Error) an *Error with the status and, where the page has a text of its own, the
// words; every other error is a 500.
//
// It is a package of its own, and not part of the page shell, because the middleware in pkg refuses
// requests too (a guard answers 403, the body limit 413) and may not import the shell.
package httperr

import (
	"errors"
	"fmt"
	"net/http"
)

// Error is a failure with the HTTP status it is answered with.
type Error struct {
	// Status is the HTTP status code of the answer.
	Status int

	// Message says what went wrong. It is logged, and it is what the page of a 413 says; every other status has
	// a text of its own, and the message is never shown there, since it can carry what the visitor has no
	// business seeing.
	Message string
}

// New is an *Error of the given status and message.
func New(status int, message string) *Error {
	return &Error{Status: status, Message: message}
}

// Error implements error. It reads "404 Not Found: no such note", or just "404 Not Found" with no message.
func (e *Error) Error() string {
	if e.Message == "" {
		return fmt.Sprintf("%d %s", e.Status, http.StatusText(e.Status))
	}

	return fmt.Sprintf("%d %s: %s", e.Status, http.StatusText(e.Status), e.Message)
}

// ErrTimeout is the cause of a request context that was cancelled because the request took longer than it was
// given (see middleware.Timeout). context.Cause(ctx) is ErrTimeout then, whereas the error a cancelled query
// returns is a plain context.Canceled that says nothing of why; the error page tells the two apart by it and
// answers 503 to the first.
var ErrTimeout = errors.New("the request took too long")
