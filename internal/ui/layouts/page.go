package layouts

import (
	"net/http"

	g "maragu.dev/gomponents"

	"github.com/tung-dnt/lean-go/internal/ui"
)

// Page writes the page, which has the content, in Primary, the layout of the site. A page that cannot be built is
// the error page (Error), and nothing of it has been sent.
func Page(w http.ResponseWriter, r *http.Request, page *ui.Request, content g.Node) {
	if err := ui.Render(w, page, Primary, content); err != nil {
		Error(w, r, err)
	}
}
