package components

import (
	g "maragu.dev/gomponents"
)

// HxBoost enables AJAX body swaps on forms and inherited navigation links.
// Inheritance is explicit in htmx v4; a form also consumes its own attribute.
func HxBoost() g.Node {
	return g.Attr("hx-boost:inherited", "true")
}
