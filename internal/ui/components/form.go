package components

import (
	g "maragu.dev/gomponents"
	h "maragu.dev/gomponents/html"

	"github.com/tung-dnt/lean-go/pkg/form"
)

type (
	// InputFieldParams are what InputField needs: a one-line input of the given InputType, named Name.
	InputFieldParams struct {
		Form        form.Form
		FormField   string
		Name        string
		InputType   string
		Label       string
		Value       string
		Placeholder string
		Help        string
	}

	// FileFieldParams are what FileField needs: a file picker named Name.
	FileFieldParams struct {
		// Form and FormField are optional. When set, errors recorded against FormField are shown under
		// the input, as for every other field. The file is not a field of the form struct, so it is
		// the handler that records them.
		Form      form.Form
		FormField string
		Name      string
		Label     string
		Help      string
		// Accept narrows the file picker to the given types, ie: "image/png,.zip". It is a convenience
		// for the user only; anything that matters must still be checked on the server.
		Accept string
	}

	// OptionsParams are what Radios and SelectList need: the Options to choose from and the Value chosen now.
	OptionsParams struct {
		Form      form.Form
		FormField string
		Name      string
		Label     string
		Value     string
		Options   []Choice
		Help      string
	}

	// Choice is one option of a Radios or SelectList: the Value sent with the form and the Label shown for it.
	Choice struct {
		Value string
		Label string
	}

	// TextareaFieldParams are what TextareaField needs: a multi-line input named Name.
	TextareaFieldParams struct {
		Form      form.Form
		FormField string
		Name      string
		Label     string
		Value     string
		Help      string
	}

	// CheckboxParams are what Checkbox needs: a box named Name, sent as "true" when Checked.
	CheckboxParams struct {
		Form      form.Form
		FormField string
		Name      string
		Label     string
		Checked   bool
	}
)

// ControlGroup lays controls, ie: buttons, out in a row.
func ControlGroup(controls ...g.Node) g.Node {
	return h.Div(
		h.Class("mt-2 flex gap-2"),
		g.Group(controls),
	)
}

// TextareaField is a labelled textarea with its help text and the errors recorded against its form field.
func TextareaField(el TextareaFieldParams) g.Node {
	return Fieldset(
		el.Label,
		h.Textarea(
			h.Class("textarea h-24 w-2/3 "+formFieldStatusClass(el.Form, el.FormField)),
			h.ID(el.Name),
			h.Name(el.Name),
			g.Text(el.Value),
		),
		Help(el.Help),
		formFieldErrors(el.Form, el.FormField),
	)
}

// Radios is a labelled set of radio buttons, one per option, with the errors recorded against its form field.
func Radios(el OptionsParams) g.Node {
	buttons := make(g.Group, len(el.Options))
	for i, opt := range el.Options {
		id := "radio-" + el.Name + "-" + opt.Value
		buttons[i] = h.Div(
			h.Class("mb-2"),
			h.Input(
				h.ID(id),
				h.Type("radio"),
				h.Name(el.Name),
				h.Value(opt.Value),
				h.Class("radio mr-1 "+formFieldStatusClass(el.Form, el.FormField)),
				g.If(el.Value == opt.Value, h.Checked()),
			),
			h.Label(
				g.Text(opt.Label),
				h.For(id),
			),
		)
	}

	return Fieldset(
		el.Label,
		buttons,
		formFieldErrors(el.Form, el.FormField),
	)
}

// SelectList is a labelled drop-down of the options, with its help text and the errors recorded against its form field.
func SelectList(el OptionsParams) g.Node {
	buttons := make(g.Group, len(el.Options))
	for i, opt := range el.Options {
		buttons[i] = h.Option(
			g.Text(opt.Label),
			h.Value(opt.Value),
			g.If(opt.Value == el.Value, g.Attr("selected")),
		)
	}

	return Fieldset(
		el.Label,
		h.Select(
			h.Class("select "+formFieldStatusClass(el.Form, el.FormField)),
			h.Name(el.Name),
			buttons,
		),
		Help(el.Help),
		formFieldErrors(el.Form, el.FormField),
	)
}

// Checkbox is a labelled checkbox with the errors recorded against its form field.
func Checkbox(el CheckboxParams) g.Node {
	return h.Div(
		h.Label(
			h.Class("label"),
			h.Input(
				h.Class("checkbox"),
				h.Type("checkbox"),
				h.Name(el.Name),
				g.If(el.Checked, h.Checked()),
				h.Value("true"),
			),
			g.Text(" "+el.Label),
		),
		formFieldErrors(el.Form, el.FormField),
	)
}

// InputField is a labelled one-line input with its help text and the errors recorded against its form field.
func InputField(el InputFieldParams) g.Node {
	return Fieldset(
		el.Label,
		h.Input(
			h.ID(el.Name),
			h.Name(el.Name),
			h.Type(el.InputType),
			h.Class("input "+formFieldStatusClass(el.Form, el.FormField)),
			h.Value(el.Value),
			g.If(el.Placeholder != "", h.Placeholder(el.Placeholder)),
		),
		Help(el.Help),
		formFieldErrors(el.Form, el.FormField),
	)
}

// Help is the small line of text under a field. It renders nothing for an empty text.
func Help(text string) g.Node {
	return g.If(len(text) > 0, h.Div(
		h.Class("label"),
		g.Text(text),
	))
}

// Fieldset groups els under label, which may be empty.
func Fieldset(label string, els ...g.Node) g.Node {
	return h.FieldSet(
		h.Class("fieldset"),
		g.If(len(label) > 0, h.Legend(
			h.Class("fieldset-legend"),
			g.Text(label),
		)),
		g.Group(els),
	)
}

// FileField is a labelled file picker with its help text and the errors recorded against its form field.
func FileField(el FileFieldParams) g.Node {
	class := "file-input"
	if el.Form != nil && el.Form.FieldHasErrors(el.FormField) {
		class += " file-input-error"
	}

	return Fieldset(
		el.Label,
		h.Input(
			h.Type("file"),
			h.Class(class),
			h.Name(el.Name),
			g.If(el.Accept != "", h.Accept(el.Accept)),
		),
		Help(el.Help),
		formFieldErrors(el.Form, el.FormField),
	)
}

func formFieldStatusClass(fm form.Form, formField string) string {
	switch {
	case fm == nil:
		return ""
	case !fm.IsSubmitted():
		return ""
	case fm.FieldHasErrors(formField):
		return "input-error"
	default:
		return "input-success"
	}
}

func formFieldErrors(fm form.Form, field string) g.Node {
	if fm == nil {
		return nil
	}

	errs := fm.GetFieldErrors(field)
	if len(errs) == 0 {
		return nil
	}

	nodes := make(g.Group, len(errs))
	for i, err := range errs {
		nodes[i] = h.Div(
			h.Class("text-error"),
			g.Text(err),
		)
	}

	return nodes
}

// FormButton is a button, for inside a form, in the color given.
func FormButton(color Color, label string) g.Node {
	return h.Button(
		h.Class("btn "+buttonColor(color)),
		g.Text(label),
	)
}

// ButtonLink is a link that looks like a button, in the color given.
func ButtonLink(color Color, href, label string) g.Node {
	return h.A(
		h.Href(href),
		h.Class("btn "+buttonColor(color)),
		g.Text(label),
	)
}

func buttonColor(color Color) string {
	// Only colors being used are included so unused styles are not compiled.
	switch color {
	case ColorPrimary:
		return "btn-primary"
	case ColorInfo:
		return "btn-info"
	case ColorAccent:
		return "btn-accent"
	case ColorError:
		return "btn-error"
	case ColorLink:
		return "btn-link"
	default:
		return ""
	}
}
