package pages

import (
	"net/http"
	"time"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/server/paths"
	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/components"
)

// Note is the note that NotePage draws.
type Note struct {
	ID        int64
	Title     string
	Body      string
	CreatedAt time.Time
}

// NotePage is one note, and for the admin the button that deletes it (GET /notes/{id}).
func NotePage(page *ui.Request, n Note) g.Node {
	page.Title = n.Title

	return h.Article(
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
	)
}
