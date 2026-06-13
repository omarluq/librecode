package tui

import (
	"strings"

	"github.com/gdamore/tcell/v3"
)

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
		return view.renderRichLines(width)
	}

	return PlainTextLines(view.Text, view.Style, width, view.Wrap)
}

func (view *TextView) renderRichLines(width int) []Line {
	if !view.Wrap {
		return append([]Line{}, view.Lines...)
	}

	lines := make([]Line, 0, len(view.Lines))
	for _, line := range view.Lines {
		lines = append(lines, line.Wrap(width)...)
	}

	return lines
}

// PlainTextLines renders plain text as styled lines.
func PlainTextLines(text string, style tcell.Style, width int, wrap bool) []Line {
	lines := make([]Line, 0, strings.Count(text, "\n")+1)
	for part := range strings.SplitSeq(text, "\n") {
		lines = append(lines, plainTextPartLines(part, style, width, wrap)...)
	}

	return lines
}

func plainTextPartLines(text string, style tcell.Style, width int, wrap bool) []Line {
	if !wrap {
		return []Line{NewLine(style, Fit(text, width))}
	}

	wrapped := Wrap(text, width)

	lines := make([]Line, 0, len(wrapped))
	for _, line := range wrapped {
		lines = append(lines, NewLine(style, line))
	}

	return lines
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
