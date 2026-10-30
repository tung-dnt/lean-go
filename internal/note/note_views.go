package note

// What the note domain draws: the list with the form of a new note, and the page of one note.

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/external/db"
	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/components"
	"github.com/tung-dnt/lean-go/internal/ui/layouts"
	"github.com/tung-dnt/lean-go/pkg/form"
	"github.com/tung-dnt/lean-go/pkg/pager"
)

// The limits of a note, which the table holds it to as well (migration 000002).
const (
	MaxTitle = 120
	MaxBody  = 5000
)

// noteForm is the form of a new note, which is also what a post of it is read into.
type noteForm struct {
	Title string `form:"title"`
	Body  string `form:"body"`
	form.Submission
}

// title is the title as it is kept: without the spaces around it.
func (f *noteForm) title() string {
	return strings.TrimSpace(f.Title)
}

// Valid implements form.Validator: a title of up to MaxTitle characters, and a body of up to MaxBody.
func (f *noteForm) Valid(context.Context) map[string]string {
	problems := map[string]string{}

	switch title := f.title(); {
	case title == "":
		problems["Title"] = form.MsgRequired
	case utf8.RuneCountInString(title) > MaxTitle:
		problems["Title"] = "Keep the title to " + strconv.Itoa(MaxTitle) + " characters."
	}
	if utf8.RuneCountInString(f.Body) > MaxBody {
		problems["Body"] = "Keep the note to " + strconv.Itoa(MaxBody) + " characters."
	}

	return problems
}

// draw writes the page, which has the content, in the layout of the site. A page that cannot be built is the error
// page, and nothing of it has been sent.
func draw(w http.ResponseWriter, r *http.Request, page *ui.Request, content g.Node) {
	if err := ui.Render(w, page, layouts.Primary, content); err != nil {
		layouts.Error(w, r, err)
	}
}

// renderList draws the form f above the page of notes that pgr is on.
func renderList(w http.ResponseWriter, r *http.Request, f *noteForm, notes []db.Note, pgr pager.Pager) {
	page := ui.NewRequest(r)
	page.Title = "Notes"

	draw(w, r, page, g.Group{
		f.render(),
		g.If(len(notes) == 0, h.P(h.Class("mt-8 opacity-70"), g.Text("No notes yet. Post the first one."))),
		h.Ul(
			h.ID("notes"),
			h.Class("mt-8 flex flex-col gap-3"),
			g.Map(notes, func(n db.Note) g.Node {
				return h.Li(
					h.A(h.Class("link link-hover font-semibold"), h.Href(paths.Note(n.ID)), g.Text(n.Title)),
					h.Span(h.Class("ml-2 text-sm opacity-60"), stamp(n.CreatedAt)),
				)
			}),
		),
		g.If(pgr.Pages > 1, h.Div(h.Class("mt-6"), components.Pager(pgr.Page, paths.Home, !pgr.IsEnd(), ""))),
	})
}

// render is the form of a new note. What is wrong with a post of it is shown under each field.
func (f *noteForm) render() g.Node {
	return h.Form(
		h.ID("new-note"),
		h.Method(http.MethodPost),
		h.Action(paths.Notes),
		components.HxBoost(),
		components.InputField(components.InputFieldParams{
			Form:      f,
			FormField: "Title",
			Name:      "title",
			InputType: "text",
			Label:     "Title",
			Value:     f.Title,
		}),
		// The count of what is typed, against MaxBody, is web/js/note/charCount.ts: data-max is its contract with
		// this markup.
		h.Div(
			g.Attr("x-data", "charCount"),
			h.Data("max", strconv.Itoa(MaxBody)),
			g.Attr("@input", "onInput"),
			components.TextareaField(components.TextareaFieldParams{
				Form:      f,
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

// renderShow draws one note, and for the admin the button that deletes it.
func renderShow(w http.ResponseWriter, r *http.Request, n db.Note) {
	page := ui.NewRequest(r)
	page.Title = n.Title

	draw(w, r, page, h.Article(
		h.ID("note"),
		h.P(h.Class("text-sm opacity-60"), stamp(n.CreatedAt)),
		h.P(h.Class("mt-4 whitespace-pre-wrap"), g.Text(n.Body)),
		components.ControlGroup(
			components.ButtonLink(components.ColorLink, paths.Home, "All notes"),
			// Only the admin can delete, and the route refuses everyone else, so the button is not even drawn for them.
			g.If(page.IsAdmin, h.Form(
				h.Method(http.MethodPost),
				h.Action(paths.AdminNoteDelete(n.ID)),
				components.HxBoost(),
				components.FormButton(components.ColorError, "Delete"),
			)),
		),
	))
}

// stamp is when a note was posted, in UTC, as a <time> that a script could make local.
func stamp(t time.Time) g.Node {
	t = t.UTC()

	return h.Time(h.DateTime(t.Format(time.RFC3339)), g.Text(t.Format("2 Jan 2006 15:04 UTC")))
}
