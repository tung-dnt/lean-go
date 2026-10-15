package components

import (
	"fmt"
	"sync/atomic"

	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

// tabGroups numbers the tab groups rendered by this process, so each one gets a radio group name of its own.
var tabGroups atomic.Uint64

// Tab is one tab: its title and its body, which is raw HTML.
type Tab struct {
	Title, Body string
}

// Tabs renders tabs as a daisyUI tab set. The radios of one set share a name, unique per render, so two sets on
// a page do not switch each other.
func Tabs(tabs []Tab) g.Node {
	nodes := make(g.Group, 0, len(tabs)*2)
	id := fmt.Sprintf("tabs-%d", tabGroups.Add(1))

	for i, tab := range tabs {
		nodes = append(nodes,
			h.Input(
				h.Type("radio"),
				h.Name(id),
				h.Class("tab"),
				h.Aria("label", tab.Title),
				g.If(i == 0, h.Checked()),
			),
			h.Div(
				h.Class("tab-content bg-base-100 border-base-300 p-6"),
				g.Raw(tab.Body),
			))
	}

	return h.Div(
		h.Class("tabs tabs-lift"),
		nodes,
	)
}
