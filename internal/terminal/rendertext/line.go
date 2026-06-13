package rendertext

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/tui"
)

// Span is a styled segment inside a line.
type Span = tui.Span

// Line is a terminal line with optional per-span styles.
type Line = tui.Line

// NewLine returns a line with one default style and no per-span overrides.
func NewLine(style tcell.Style, text string) Line {
	return tui.NewLine(style, text)
}

// SafeTail returns at most maxItems tail items. Negative maxItems means no limit.
func SafeTail[T any](items []T, maxItems int) []T {
	return tui.Tail(items, maxItems)
}
