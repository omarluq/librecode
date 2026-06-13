package rendertext

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/tui"
)

// Segment is one display grapheme with its terminal cell width.
type Segment = tui.Segment

// ContentSetter is the subset of tcell.Screen used by text rendering helpers.
type ContentSetter = tui.Screen

// RuneLen returns the number of UTF-8 runes in text.
func RuneLen(text string) int { return tui.RuneLen(text) }

// Width returns the terminal display width of text.
func Width(text string) int { return tui.Width(text) }

// Segments splits text into terminal grapheme segments.
func Segments(text string) []Segment { return tui.Segments(text) }

// Truncate fits text into width cells, appending an ellipsis when truncated.
func Truncate(text string, width int) string { return tui.Truncate(text, width) }

// Fit returns the longest prefix of text that fits into width terminal cells.
func Fit(text string, width int) string { return tui.Fit(text, width) }

// PadRight fits text to width cells and pads the rest with ASCII spaces.
func PadRight(text string, width int) string { return tui.PadRight(text, width) }

// Wrap wraps text to width cells, trimming wrapping whitespace.
func Wrap(text string, width int) []string { return tui.Wrap(text, width) }

// WrapPreserveWhitespace wraps text to width cells without trimming wrapping whitespace.
func WrapPreserveWhitespace(text string, width int) []string {
	return tui.WrapPreserveWhitespace(text, width)
}

// WrapBreakIndex returns the segment boundary for one wrapped line.
func WrapBreakIndex(segments []Segment, width int) int { return tui.WrapBreakIndex(segments, width) }

// JoinSegments concatenates segment text.
func JoinSegments(segments []Segment) string { return tui.JoinSegments(segments) }

const borderHorizontalPadding = 2

// TopBorder returns a rounded top border with an optional right-aligned label.
func TopBorder(width int, label string) string {
	innerWidth := max(1, width-borderHorizontalPadding)

	label = strings.TrimSpace(label)
	if label == "" {
		return "╭" + strings.Repeat("─", innerWidth) + "╮"
	}

	label = strings.ReplaceAll(label, "\n", " ")
	suffix := Truncate(label+"──", innerWidth)
	fillWidth := max(0, innerWidth-Width(suffix))

	return "╭" + strings.Repeat("─", fillWidth) + suffix + "╮"
}

// MiddleBorder returns a horizontal separator border.
func MiddleBorder(width int) string {
	return "├" + strings.Repeat("─", max(1, width-borderHorizontalPadding)) + "┤"
}

// BottomBorder returns a rounded bottom border.
func BottomBorder(width int) string {
	return "╰" + strings.Repeat("─", max(1, width-borderHorizontalPadding)) + "╯"
}

// WriteCells writes text into exactly width cells, filling remaining cells with spaces.
func WriteCells(screen ContentSetter, column, row, width int, text string, style tcell.Style) int {
	return tui.WriteCells(screen, column, row, width, text, style)
}

// WriteCellsNoFill writes as much text as fits in width cells without filling remaining cells.
func WriteCellsNoFill(screen ContentSetter, column, row, width int, text string, style tcell.Style) int {
	return tui.WriteCellsNoFill(screen, column, row, width, text, style)
}

// WriteSegment writes one terminal grapheme segment.
func WriteSegment(screen ContentSetter, column, row, width int, segment Segment, style tcell.Style) int {
	return tui.WriteSegment(screen, column, row, width, segment, style)
}

// WriteTabSegment writes a tab as up to four spaces.
func WriteTabSegment(screen ContentSetter, column, row, width int, style tcell.Style) int {
	return tui.WriteTabSegment(screen, column, row, width, style)
}
