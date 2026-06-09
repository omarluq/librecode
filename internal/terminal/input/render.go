package input

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/rendertext"
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
	innerWidth := max(1, width-4)
	bodyLines := BodyLines(value, innerWidth)
	cursorRow, cursorColumn := CursorPosition(value, cursor, innerWidth)
	visibleLines, skippedRows := VisibleLines(bodyLines, maxRows, cursorRow)
	lines := make([]rendertext.Line, 0, len(visibleLines)+2)
	lines = append(lines, rendertext.NewLine(borderStyle, rendertext.TopBorder(width, label)))
	for _, bodyLine := range visibleLines {
		bodyText := rendertext.PadRight(bodyLine, innerWidth)
		lines = append(lines, rendertext.NewLine(bodyStyle, "│ "+bodyText+" │"))
	}
	lines = append(lines, rendertext.NewLine(borderStyle, rendertext.BottomBorder(width)))

	return Render{
		Lines:     lines,
		CursorCol: 2 + cursorColumn,
		CursorRow: 1 + cursorRow - skippedRows,
	}
}

// BodyLines wraps the editor body text into display lines.
func BodyLines(value []rune, width int) []string {
	if len(value) == 0 {
		return []string{""}
	}

	return WrapText(string(value), width)
}

// CursorPosition returns the display row/column for cursor.
func CursorPosition(value []rune, cursor, width int) (row, column int) {
	cursor = ClampCursor(cursor, len(value))
	prefix := string(value[:cursor])
	lines := WrapText(prefix, width)
	if len(lines) == 0 {
		return 0, 0
	}
	lastLine := lines[len(lines)-1]
	if strings.HasSuffix(prefix, "\n") {
		return len(lines) - 1, 0
	}

	return len(lines) - 1, rendertext.Width(lastLine)
}

// WrapText wraps text by display width while preserving logical newlines.
func WrapText(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	logicalLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(logicalLines))
	for _, logicalLine := range logicalLines {
		lines = append(lines, WrapLogicalLine(logicalLine, width)...)
	}

	return lines
}

// WrapLogicalLine wraps one logical line by display width.
func WrapLogicalLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}

	segments := rendertext.Segments(line)
	lines := []string{}
	for len(segments) > 0 {
		breakIndex := rendertext.WrapBreakIndex(segments, width)
		lines = append(lines, rendertext.JoinSegments(segments[:breakIndex]))
		segments = segments[breakIndex:]
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}

	return lines
}

// VisibleLines returns the visible viewport for lines and cursor position.
func VisibleLines(lines []string, maxRows, cursorRow int) (visible []string, skippedRows int) {
	if maxRows < 1 || len(lines) <= maxRows {
		return lines, 0
	}
	start := max(0, cursorRow-maxRows+1)
	if start+maxRows > len(lines) {
		start = len(lines) - maxRows
	}

	return lines[start : start+maxRows], start
}
