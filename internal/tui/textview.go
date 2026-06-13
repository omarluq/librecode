package tui

import "github.com/gdamore/tcell/v3"

// TextView displays wrapped plain or styled text.
type TextView struct {
	Style  tcell.Style
	Text   string
	Lines  []Line
	Scroll int
	Wrap   bool
}

// NewTextView returns a text view with plain text.
func NewTextView(text string) *TextView {
	return &TextView{Text: text, Lines: nil, Style: tcell.Style{}, Wrap: true, Scroll: 0}
}

// SetLines replaces rich text lines.
func (view *TextView) SetLines(lines []Line) *TextView {
	if view == nil {
		return nil
	}

	view.Lines = append([]Line{}, lines...)
	view.Text = ""

	return view
}

// Render returns the visible text lines for width and height.
func (view *TextView) Render(width, height int) []Line {
	if view == nil || width <= 0 || height <= 0 {
		return []Line{}
	}

	lines := view.renderAll(width)
	if len(lines) == 0 {
		return []Line{}
	}

	start := min(max(0, view.Scroll), max(0, len(lines)-1))
	end := min(start+height, len(lines))

	return lines[start:end]
}

// Draw draws the visible text view.
func (view *TextView) Draw(screen ContentSetter, rect Rect) {
	DrawLines(screen, rect, view.Render(rect.Width, rect.Height))
}

func (view *TextView) renderAll(width int) []Line {
	if len(view.Lines) > 0 {
		if !view.Wrap {
			return append([]Line{}, view.Lines...)
		}

		lines := []Line{}
		for _, line := range view.Lines {
			lines = append(lines, line.Wrap(width)...)
		}

		return lines
	}

	wrapped := []Line{}

	if view.Wrap {
		for _, line := range Wrap(view.Text, width) {
			wrapped = append(wrapped, NewLine(view.Style, line))
		}

		return wrapped
	}

	for _, line := range WrapPreserveWhitespace(view.Text, max(1, width)) {
		wrapped = append(wrapped, NewLine(view.Style, line))
	}

	return wrapped
}

// RichText is a collection of styled lines.
type RichText struct {
	Lines []Line
}

// NewRichText returns rich text from lines.
func NewRichText(lines []Line) RichText {
	return RichText{Lines: append([]Line{}, lines...)}
}

// RichTextArea is a text area paired with rich preview lines.
type RichTextArea struct {
	Preview []Line
	TextArea
}
