// Package domain is what the note domain knows without a database or a request: how many notes a page shows, the
// limits of a note and what is wrong with one that is over them, the id of a path, and the one error of the
// business. It imports no HTTP, JSON or database package, and neither the business root (internal/note) nor the
// adapters.
package domain

import (
	"errors"
	"strconv"
	"strings"
	"unicode/utf8"

	"github.com/tung-dnt/lean-go/pkg/form"
)

// PerPage is how many notes a page of the list shows.
const PerPage = 20

// The limits of a note, which the table holds it to as well (migration 000002).
const (
	MaxTitle = 120
	MaxBody  = 5000
)

// ErrNotFound is the answer for an id that is no note's.
var ErrNotFound = errors.New("no such note")

// CleanTitle is the title as it is kept: without the spaces around it.
func CleanTitle(title string) string {
	return strings.TrimSpace(title)
}

// TitleProblem says what is wrong with a title, or "" when nothing is: it has to be there, and to be of up to
// MaxTitle characters.
func TitleProblem(title string) string {
	title = CleanTitle(title)

	switch {
	case title == "":
		return form.MsgRequired
	case utf8.RuneCountInString(title) > MaxTitle:
		return "Keep the title to " + strconv.Itoa(MaxTitle) + " characters."
	}

	return ""
}

// BodyProblem says what is wrong with a body, or "" when nothing is: it is of up to MaxBody characters.
func BodyProblem(body string) string {
	if utf8.RuneCountInString(body) > MaxBody {
		return "Keep the note to " + strconv.Itoa(MaxBody) + " characters."
	}

	return ""
}

// ParseID is the id of a path, which is a positive number or nothing.
func ParseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)

	return id, err == nil && id > 0
}
