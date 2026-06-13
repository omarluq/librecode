package tui

import "github.com/gdamore/tcell/v3"

// Modal draws a centered boxed component.
type Modal struct {
	BoxStyle tcell.Style
	Child    Drawer
	Title    string
	Width    int
	Height   int
}

// Draw draws the modal.
func (modal *Modal) Draw(screen ContentSetter, rect Rect) {
	if modal == nil || screen == nil || rect.Empty() {
		return
	}

	width := modal.Width
	if width <= 0 || width > rect.Width {
		width = rect.Width
	}

	height := modal.Height
	if height <= 0 || height > rect.Height {
		height = rect.Height
	}

	modalRect := Rect{
		X:      rect.X + (rect.Width-width)/2,
		Y:      rect.Y + (rect.Height-height)/2,
		Width:  width,
		Height: height,
	}
	box := &Box{Title: modal.Title, Style: modal.BoxStyle, Border: RoundedBorder()}
	box.Draw(screen, modalRect)

	if modal.Child != nil {
		modal.Child.Draw(screen, modalRect.Inner(1))
	}
}

// FormField is a simple labeled text field.
type FormField struct {
	Label string
	Value string
}

// Form displays labeled fields. Editing behavior can be layered on top later.
type Form struct {
	Style      tcell.Style
	LabelStyle tcell.Style
	Title      string
	Fields     []FormField
}

// Render returns form lines.
func (form *Form) Render(width, height int) []Line {
	if form == nil || width <= 0 || height <= 0 {
		return []Line{}
	}

	lines := []Line{}
	if form.Title != "" {
		lines = append(lines, NewLine(form.LabelStyle.Bold(true), Truncate(form.Title, width)))
	}

	for _, field := range form.Fields {
		text := field.Label + ": " + field.Value
		lines = append(lines, NewLine(form.Style, Truncate(text, width)))
	}

	return Tail(lines, height)
}

// Draw draws the form.
func (form *Form) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, form.Render(rect.Width, rect.Height))
}
