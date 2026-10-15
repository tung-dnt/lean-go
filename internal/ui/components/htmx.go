package components

import (
	g "maragu.dev/gomponents"
)

// HxBoost enables htmx boosting so anchors/forms navigate via AJAX body swaps
// instead of full page loads. The behavior it triggers (view transitions,
// progress bar, error swapping) lives in web/js/app.ts.
func HxBoost() g.Node {
	return g.Attr("hx-boost", "true")
}
