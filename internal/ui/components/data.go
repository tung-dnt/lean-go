package components

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"
)

type (
	// CardParams are what Card needs. Body and Footer may be empty; the footer is only drawn when there is one.
	CardParams struct {
		Title  string
		Body   g.Group
		Footer g.Group
		Color  Color
		Size   Size
	}

	// Stat is one figure of Stats: a Title over a Value, with a Description under it and an optional Icon.
	Stat struct {
		Title       string
		Value       string
		Description string
		Icon        g.Node
	}
)

// Badge is a small label in the color given, success or warning. Any other color has no style.
func Badge(color Color, text string) g.Node {
	var class string

	switch color {
	case ColorSuccess:
		class = "badge-success"
	case ColorWarning:
		class = "badge-warning"
	}

	return h.Div(
		h.Class("badge "+class),
		g.Text(text),
	)
}

// Divider is a horizontal rule with text in the middle of it.
func Divider(text string) g.Node {
	return h.Div(
		h.Class("divider"),
		g.Text(text),
	)
}

// Card is a box with an optional title, a body and an optional footer of actions.
func Card(params CardParams) g.Node {
	var colorClass, sizeClass string

	switch params.Color {
	case ColorSuccess:
		colorClass = "bg-success text-success-content"
	case ColorPrimary:
		colorClass = "bg-primary text-primary-content"
	case ColorAccent:
		colorClass = "bg-accent text-accent-content"
	case ColorNeutral:
		colorClass = "bg-neutral text-neutral-content"
	case ColorWarning:
		colorClass = "bg-warning text-warning-content"
	case ColorInfo:
		colorClass = "bg-info text-info-content"
	}

	switch params.Size {
	case SizeSmall:
		sizeClass = "card-sm"
	case SizeMedium:
		sizeClass = "card-md"
	case SizeLarge:
		sizeClass = "card-lg"
	}

	return h.Div(
		h.Class("cards mb-2 "+colorClass+" "+sizeClass),
		h.Div(
			h.Class("card-body"),
			g.If(len(params.Title) > 0, h.Span(
				h.Class("card-title"),
				g.Text(params.Title),
			)),
			params.Body,
			g.If(params.Footer != nil, h.Div(
				h.Class("card-actions justify-end"),
				params.Footer,
			)),
		),
	)
}

// Stats is a row of stats.
func Stats(stats ...Stat) g.Node {
	items := make(g.Group, 0, len(stats))
	for _, stat := range stats {
		items = append(items, h.Div(
			h.Class("stat"),
			g.Iff(stat.Icon != nil, func() g.Node {
				return h.Div(
					h.Class("stat-figure text-secondary"),
					stat.Icon,
				)
			}),
			h.Div(
				h.Class("stat-title"),
				g.Text(stat.Title),
			),
			h.Div(
				h.Class("stat-value"),
				g.Text(stat.Value),
			),
			h.Div(
				h.Class("stat-desc"),
				g.Text(stat.Description),
			),
		))
	}
	return h.Div(
		h.Class("stats shadow"),
		items,
	)
}
