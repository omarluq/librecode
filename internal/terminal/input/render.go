package input

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/rendertext"
	"github.com/omarluq/librecode/internal/tui"
)

// Render describes rendered editor lines and cursor position.
type Render struct {
	Lines     []rendertext.Line
	CursorCol int
	CursorRow int
}

// RenderEditor renders the editable composer with a border.
func RenderEditor(
	value []rune,
	cursor int,
	width int,
	maxRows int,
	borderStyle tcell.Style,
	bodyStyle tcell.Style,
	label string,
) Render {
	rendered := tui.RenderTextArea(
		value,
		cursor,
		width,
		maxRows,
		tui.TextAreaStyles{Border: borderStyle, Body: bodyStyle},
		label,
	)

	return Render{Lines: rendered.Lines, CursorCol: rendered.CursorCol, CursorRow: rendered.CursorRow}
}

// BodyLines wraps the editor body text into display lines.
func BodyLines(value []rune, width int) []string { return tui.TextAreaBodyLines(value, width) }

// CursorPosition returns the display row/column for cursor.
func CursorPosition(value []rune, cursor, width int) (row, column int) {
	return tui.TextAreaCursorPosition(value, cursor, width)
}

// WrapText wraps text by display width while preserving logical newlines.
func WrapText(text string, width int) []string { return tui.WrapPreserveWhitespace(text, width) }

// VisibleLines returns the visible viewport for lines and cursor position.
func VisibleLines(lines []string, maxRows, cursorRow int) (visible []string, skippedRows int) {
	return tui.VisibleLines(lines, maxRows, cursorRow)
}
