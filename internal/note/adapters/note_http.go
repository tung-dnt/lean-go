// Package adapters is the presentation of the note domain: the handlers that read a request and answer with a page
// (note_http.go), the form of a new note (note_form.go) and the gomponents of the pages (note_views.go). A handler
// calls the business root (internal/note) with plain parameters, and gives the status of the web to what the
// business answers: a note that is not there is a 404.
//
// HandleX(deps...) takes only what it uses; internal/server/routes.go lists every route.
package adapters

import (
	"errors"
	"net/http"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/note"
	"github.com/tung-dnt/lean-go/internal/note/domain"
	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/pager"
	"github.com/tung-dnt/lean-go/pkg/redirect"
)

// errNotFound is the answer for an id that is no note's, a malformed one included: the two cannot be told apart from
// outside.
var errNotFound = httperr.New(http.StatusNotFound, domain.ErrNotFound.Error())

// HandleList is the list of notes, newest first, with the form of a new one above it (GET /).
func HandleList(q *db.Queries) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		renderListOrFail(w, r, q, &noteForm{})
	})
}

// HandleCreate posts a note (POST /notes). A form that is not filled in right is answered with the list and the form
// again, with what was entered and what is wrong with it; a note that is made is answered with a 303 to its page, so
// that a reload of that page does not post it again.
func HandleCreate(q *db.Queries) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var in noteForm
		if err := form.Decode(r, &in); err != nil {
			layouts.Error(w, r, err)

			return
		}
		if !in.IsValid() {
			renderListOrFail(w, r, q, &in)

			return
		}

		n, err := note.Create(r.Context(), q, in.Title, in.Body)
		if err != nil {
			layouts.Error(w, r, err)

			return
		}

		msg.Success(r.Context(), "Your note is posted.")
		redirect.To(w, r, paths.Note(n.ID), http.StatusSeeOther)
	})
}

// HandleShow is the page of one note (GET /notes/{id}).
func HandleShow(q *db.Queries) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := domain.ParseID(r.PathValue("id"))
		if !ok {
			layouts.Error(w, r, errNotFound)

			return
		}

		n, err := note.Get(r.Context(), q, id)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			layouts.Error(w, r, errNotFound)

			return
		case err != nil:
			layouts.Error(w, r, err)

			return
		}

		renderShow(w, r, n)
	})
}

// HandleDelete deletes a note (POST /admin/notes/{id}/delete) and sends the admin back to the list. It is below
// /admin, so the server has held the request to the admin check before it gets here, and it is a post, since a get
// that changes something is fired by whatever fetches links. A note that is already gone is a 404.
func HandleDelete(q *db.Queries) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := domain.ParseID(r.PathValue("id"))
		if !ok {
			layouts.Error(w, r, errNotFound)

			return
		}

		err := note.Delete(r.Context(), q, id)
		switch {
		case errors.Is(err, domain.ErrNotFound):
			layouts.Error(w, r, errNotFound)

			return
		case err != nil:
			layouts.Error(w, r, err)

			return
		}

		msg.Success(r.Context(), "The note is deleted.")
		redirect.To(w, r, paths.Home, http.StatusSeeOther)
	})
}

// renderListOrFail loads the page of the list the request asks for and draws it with the form f, or the error page
// when it cannot be loaded.
func renderListOrFail(w http.ResponseWriter, r *http.Request, q *db.Queries, f *noteForm) {
	pgr := pager.NewPager(r, domain.PerPage)

	notes, err := note.List(r.Context(), q, &pgr)
	if err != nil {
		layouts.Error(w, r, err)

		return
	}

	renderList(w, r, f, notes, pgr)
}
