package pages

import (
	"net/http"
	"strconv"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/components"
	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/pager"
)

// NoteLink is one line of the list of notes: the note's title, as a link to its page, and when it was posted.
type NoteLink struct {
	ID        int64
	Title     string
	CreatedAt time.Time
}

// NewNote is the form of a new note that HomePage draws: Form holds what is wrong with a post of it, shown under
// each field, Title and Body are what was entered, and BodyLimit is the most characters that a body may have.
type NewNote struct {
	Form      form.Form
	Title     string
	Body      string
	BodyLimit int
}

// HomePage is the list of notes (GET /, and the answer to a post of a note that is not made): the form of a new
// note above the page of notes that pgr is on.
func HomePage(page *ui.Request, f NewNote, notes []NoteLink, pgr pager.Pager) g.Node {
	page.Title = "Notes"

	return g.Group{
		newNoteForm(f),
		g.If(len(notes) == 0, h.P(h.Class("mt-8 opacity-70"), g.Text("No notes yet. Post the first one."))),
		h.Ul(
			h.ID("notes"),
			h.Class("mt-8 flex flex-col gap-3"),
			g.Map(notes, func(n NoteLink) g.Node {
				return h.Li(
					h.A(h.Class("link link-hover font-semibold"), h.Href(paths.Note(n.ID)), g.Text(n.Title)),
					h.Span(h.Class("ml-2 text-sm opacity-60"), stamp(n.CreatedAt)),
				)
			}),
		),
		g.If(pgr.Pages > 1, h.Div(h.Class("mt-6"), components.Pager(pgr.Page, paths.Home, !pgr.IsEnd(), ""))),
	}
}

// newNoteForm is the form of a new note. What is wrong with a post of it is shown under each field.
func newNoteForm(f NewNote) g.Node {
	return h.Form(
		h.ID("new-note"),
		h.Method(http.MethodPost),
		h.Action(paths.Notes),
		components.HxBoost(),
		components.InputField(components.InputFieldParams{
			Form:      f.Form,
			FormField: "Title",
			Name:      "title",
			InputType: "text",
			Label:     "Title",
			Value:     f.Title,
		}),
		// The count of what is typed, against the limit of the body, is web/js/note/charCount.ts: data-max is its
		// contract with this markup.
		h.Div(
			g.Attr("x-data", "charCount"),
			h.Data("max", strconv.Itoa(f.BodyLimit)),
			g.Attr("@input", "onInput"),
			components.TextareaField(components.TextareaFieldParams{
				Form:      f.Form,
				FormField: "Body",
				Name:      "body",
				Label:     "Note",
				Value:     f.Body,
			}),
			h.P(h.Class("text-sm opacity-60"), g.Attr("x-text", "count + ' / ' + max")),
		),
		components.ControlGroup(components.FormButton(components.ColorPrimary, "Post")),
	)
}

// stamp is when a note was posted, in UTC, as a <time> that a script could make local.
func stamp(t time.Time) g.Node {
	t = t.UTC()

	return h.Time(h.DateTime(t.Format(time.RFC3339)), g.Text(t.Format("2 Jan 2006 15:04 UTC")))
}
