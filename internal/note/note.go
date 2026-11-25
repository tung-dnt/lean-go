// Package note is the example domain of the template: anyone may post a note, everyone sees the list and each note,
// and the admin may delete one. It is here to show the shape of a domain, not to be kept: copy it to start a new one
// and delete it when the first real domain exists.
//
// A domain is three packages. This one is its business (note.go): it calls the generated sqlc queries directly and
// answers with the rows and with the errors of note/domain, never with a status or a page. note/domain holds the
// rules, and note/adapters the handlers, the form and the views that internal/server/routes.go routes to; they call
// this package with plain parameters. Tests use the external package (note_test) through internal/server/servertest.
// Multi-step workflows get concern-prefixed definition and step files here.
//
// note is a leaf domain: it imports no other.
package note

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/note/domain"
	"github.com/tung-dnt/lean-go/pkg/pager"
)

// List is the page of notes that pgr asks for, newest first. A page past the end is the last one.
func List(ctx context.Context, q *db.Queries, pgr *pager.Pager) ([]db.Note, error) {
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

// Get is the note with the id, or domain.ErrNotFound when no note has it.
func Get(ctx context.Context, q *db.Queries, id int64) (db.Note, error) {
	n, err := q.GetNote(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return db.Note{}, domain.ErrNotFound
	}
	if err != nil {
		return db.Note{}, fmt.Errorf("get note: %w", err)
	}

	return n, nil
}

// Create posts a note. The title is kept without the spaces around it.
func Create(ctx context.Context, q *db.Queries, title, body string) (db.Note, error) {
	n, err := q.CreateNote(ctx, db.CreateNoteParams{Title: domain.CleanTitle(title), Body: body})
	if err != nil {
		return db.Note{}, fmt.Errorf("create note: %w", err)
	}

	return n, nil
}

// Delete deletes the note with the id, or says domain.ErrNotFound when no note has it, one that is already gone
// included.
func Delete(ctx context.Context, q *db.Queries, id int64) error {
	deleted, err := q.DeleteNote(ctx, id)
	if err != nil {
		return fmt.Errorf("delete note: %w", err)
	}
	if deleted == 0 {
		return domain.ErrNotFound
	}

	return nil
}
