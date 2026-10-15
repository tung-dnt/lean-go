// Package components holds the building blocks of the pages: form fields, alerts, cards, tabs, the pager, the
// menu link and what goes in the head of a page. They return gomponents nodes.
package components

type (
	// Color is the color of a component. ColorNone leaves it to the component's own style.
	Color int
	// Size is the size of a component.
	Size int
)

// The colors a component can have.
const (
	ColorNone Color = iota
	ColorNeutral
	ColorPrimary
	ColorSecondary
	ColorAccent
	ColorInfo
	ColorSuccess
	ColorWarning
	ColorError
	ColorLink
)

// The sizes a component can have.
const (
	SizeExtraSmall Size = iota
	SizeSmall
	SizeMedium
	SizeLarge
	SizeExtraLarge
)
