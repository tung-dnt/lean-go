// Package note is the example domain of the template: anyone may post a note, everyone sees the list and each note,
// and the admin may delete one. It is here to show the shape of a domain, not to be kept: copy it to start a new one
// and delete it when the first real domain exists.
//
// A domain is one flat package: <domain>.go holds the handler makers and the logic, views.go the gomponents, and the
// tests are end to end, in the external package (note_test) through internal/server/servertest. A handler is made by
// HandleX(deps...) http.Handler, taking only what it uses, and internal/server/routes.go lists its routes.
//
// note is a leaf domain: it imports no other.
package note

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strconv"

	"github.com/jackc/pgx/v5"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/httperr"
	"github.com/tung-dnt/lean-go/pkg/msg"
	"github.com/tung-dnt/lean-go/pkg/pager"
	"github.com/tung-dnt/lean-go/pkg/paths"
	"github.com/tung-dnt/lean-go/pkg/redirect"
)

// PerPage is how many notes a page of the list shows.
const PerPage = 20

// errNotFound is the answer for an id that is no note's, a malformed one included: the two cannot be told apart from
// outside.
var errNotFound = httperr.New(http.StatusNotFound, "no such note")

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

		n, err := q.CreateNote(r.Context(), db.CreateNoteParams{Title: in.title(), Body: in.Body})
		if err != nil {
			layouts.Error(w, r, fmt.Errorf("create note: %w", err))

			return
		}

		msg.Success(r.Context(), "Your note is posted.")
		redirect.To(w, r, paths.Note(n.ID), http.StatusSeeOther)
	})
}

// HandleShow is the page of one note (GET /notes/{id}).
func HandleShow(q *db.Queries) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id, ok := parseID(r.PathValue("id"))
		if !ok {
			layouts.Error(w, r, errNotFound)

			return
		}

		n, err := q.GetNote(r.Context(), id)
		switch {
		case errors.Is(err, pgx.ErrNoRows):
			layouts.Error(w, r, errNotFound)

			return
		case err != nil:
			layouts.Error(w, r, fmt.Errorf("get note: %w", err))

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
		id, ok := parseID(r.PathValue("id"))
		if !ok {
			layouts.Error(w, r, errNotFound)

			return
		}

		n, err := q.DeleteNote(r.Context(), id)
		switch {
		case err != nil:
			layouts.Error(w, r, fmt.Errorf("delete note: %w", err))

			return
		case n == 0:
			layouts.Error(w, r, errNotFound)

			return
		}

		msg.Success(r.Context(), "The note is deleted.")
		redirect.To(w, r, paths.Home, http.StatusSeeOther)
	})
}

// renderListOrFail loads the page of the list the request asks for and draws it with the form f, or the error page
// when it cannot be loaded.
func renderListOrFail(w http.ResponseWriter, r *http.Request, q *db.Queries, f *noteForm) {
	pgr := pager.NewPager(r, PerPage)

	notes, err := loadList(r.Context(), q, &pgr)
	if err != nil {
		layouts.Error(w, r, err)

		return
	}

	renderList(w, r, f, notes, pgr)
}

// loadList is the page of notes that pgr asks for. A page past the end is the last one.
func loadList(ctx context.Context, q *db.Queries, pgr *pager.Pager) ([]db.Note, error) {
	total, err := q.CountNotes(ctx)
	if err != nil {
		return nil, fmt.Errorf("count notes: %w", err)
	}
	// Before the offset is read: this is what pulls a page past the end back onto the last one.
	pgr.SetItems(int(total))

	notes, err := q.ListNotes(ctx, db.ListNotesParams{
		PageLimit:  int64(pgr.ItemsPerPage),
		PageOffset: int64(pgr.GetOffset()),
	})
	if err != nil {
		return nil, fmt.Errorf("list notes: %w", err)
	}

	return notes, nil
}

// parseID is the id of a path, which is a positive number or nothing.
func parseID(s string) (int64, bool) {
	id, err := strconv.ParseInt(s, 10, 64)

	return id, err == nil && id > 0
}
