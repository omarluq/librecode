package rendertext

import "github.com/gdamore/tcell/v3"

// Span is a styled segment inside a line.
type Span struct {
	Style tcell.Style
	Text  string
}

// Line is a terminal line with optional per-span styles.
type Line struct {
	Style tcell.Style
	Text  string
	Spans []Span // optional per-segment styles for syntax-highlighted or mixed-style lines
}

// NewLine returns a line with one default style and no per-span overrides.
func NewLine(style tcell.Style, text string) Line {
	return Line{Style: style, Text: text, Spans: nil}
}

// SafeTail returns at most maxItems tail items. Negative maxItems means no limit.
func SafeTail[T any](items []T, maxItems int) []T {
	if maxItems < 0 || len(items) <= maxItems {
		return items
	}

	return items[len(items)-maxItems:]
}
