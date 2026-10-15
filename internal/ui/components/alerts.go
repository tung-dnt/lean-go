package components

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/internal/ui"
	"github.com/tung-dnt/lean-go/internal/ui/icons"
	"github.com/tung-dnt/lean-go/pkg/msg"
)

// FlashMessages renders the messages queued for the request (pkg/msg) as alerts, one per message, in the
// order success, info, warning, error.
func FlashMessages(r *ui.Request) g.Node {
	var nodes g.Group
	var color Color

	for _, typ := range []msg.Type{
		msg.TypeSuccess,
		msg.TypeInfo,
		msg.TypeWarning,
		msg.TypeError,
	} {
		for _, str := range msg.Get(r.Context, typ) {
			switch typ {
			case msg.TypeSuccess:
				color = ColorSuccess
			case msg.TypeInfo:
				color = ColorInfo
			case msg.TypeWarning:
				color = ColorWarning
			case msg.TypeError:
				color = ColorError
			}

			nodes = append(nodes, Alert(color, str))
		}
	}

	return nodes
}

// Alert is a dismissible message in the color given: success, info, warning or error. Any other color has no style.
func Alert(color Color, text string) g.Node {
	var class string

	switch color {
	case ColorSuccess:
		class = "alert-success"
	case ColorInfo:
		class = "alert-info"
	case ColorWarning:
		class = "alert-warning"
	case ColorError:
		class = "alert-error"
	}

	return h.Div(
		h.Role("alert"),
		h.Class("alert mb-2 "+class),
		g.Attr("x-data", "{show: true}"),
		g.Attr("x-show", "show"),
		h.Span(
			g.Attr("@click", "show = false"),
			h.Class("cursor-pointer"),
			icons.XCircle(),
		),
		h.Span(g.Text(text)),
	)
}
