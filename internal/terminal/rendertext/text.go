package rendertext

import (
	"strings"
	"unicode/utf8"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
)

// Segment is one display grapheme with its terminal cell width.
type Segment struct {
	Text  string
	Width int
}

// ContentSetter is the subset of tcell.Screen used by text rendering helpers.
type ContentSetter interface {
	SetContent(column, row int, mainc rune, combc []rune, style tcell.Style)
}

// RuneLen returns the number of UTF-8 runes in text.
func RuneLen(text string) int {
	return utf8.RuneCountInString(text)
}

// Width returns the terminal display width of text.
func Width(text string) int {
	if text == "" {
		return 0
	}

	return displaywidth.String(text)
}

// Segments splits text into terminal grapheme segments.
func Segments(text string) []Segment {
	segments := []Segment{}

	iterator := displaywidth.StringGraphemes(text)
	for iterator.Next() {
		segments = append(segments, Segment{
			Text:  iterator.Value(),
			Width: max(0, iterator.Width()),
		})
	}

	return segments
}

// Truncate fits text into width cells, appending an ellipsis when truncated.
func Truncate(text string, width int) string {
	if width <= 0 || text == "" {
		return ""
	}

	if Width(text) <= width {
		return text
	}

	if width == 1 {
		return "…"
	}

	return Fit(text, width-1) + "…"
}

// Fit returns the longest prefix of text that fits into width terminal cells.
func Fit(text string, width int) string {
	if width <= 0 || text == "" {
		return ""
	}

	var builder strings.Builder

	used := 0

	for _, segment := range Segments(text) {
		if segment.Width == 0 {
			builder.WriteString(segment.Text)

			continue
		}

		if used+segment.Width > width {
			break
		}

		builder.WriteString(segment.Text)
		used += segment.Width
	}

	return builder.String()
}

// PadRight fits text to width cells and pads the rest with ASCII spaces.
func PadRight(text string, width int) string {
	if width <= 0 {
		return ""
	}

	text = Fit(text, width)

	padding := width - Width(text)
	if padding <= 0 {
		return text
	}

	return text + strings.Repeat(" ", padding)
}

// Wrap wraps text to width cells, trimming wrapping whitespace.
func Wrap(text string, width int) []string {
	return wrapWithMode(text, width, false)
}

// WrapPreserveWhitespace wraps text to width cells without trimming wrapping whitespace.
func WrapPreserveWhitespace(text string, width int) []string {
	return wrapWithMode(text, width, true)
}

func wrapWithMode(text string, width int, preserveWhitespace bool) []string {
	if width <= 0 {
		return []string{""}
	}

	logicalLines := strings.Split(text, "\n")

	lines := make([]string, 0, len(logicalLines))
	for _, logicalLine := range logicalLines {
		lines = append(lines, wrapLogicalLineWithMode(logicalLine, width, preserveWhitespace)...)
	}

	return lines
}

func wrapLogicalLineWithMode(line string, width int, preserveWhitespace bool) []string {
	if line == "" {
		return []string{""}
	}

	segments := Segments(line)
	lines := []string{}

	for len(segments) > 0 {
		breakIndex := WrapBreakIndex(segments, width)

		wrapped := JoinSegments(segments[:breakIndex])
		if !preserveWhitespace {
			wrapped = strings.TrimRight(wrapped, " ")
		}

		lines = append(lines, wrapped)

		segments = segments[breakIndex:]
		if !preserveWhitespace {
			segments = trimLeadingSpaces(segments)
		}
	}

	if len(lines) == 0 {
		lines = append(lines, "")
	}

	return lines
}

// WrapBreakIndex returns the segment boundary for one wrapped line.
func WrapBreakIndex(segments []Segment, width int) int {
	used := 0
	limit := 0
	lastSpace := -1

	for limit < len(segments) {
		segment := segments[limit]

		segmentWidth := segment.Width
		if segmentWidth == 0 {
			limit++

			continue
		}

		if used+segmentWidth > width {
			break
		}

		used += segmentWidth

		if segmentIsSpace(segment) {
			lastSpace = limit
		}

		limit++
	}

	if limit == 0 {
		return 1
	}

	if limit < len(segments) && lastSpace > 0 {
		return lastSpace + 1
	}

	return limit
}

func segmentIsSpace(segment Segment) bool {
	return segment.Text == " " || segment.Text == "\t"
}

func trimLeadingSpaces(segments []Segment) []Segment {
	for len(segments) > 0 && segmentIsSpace(segments[0]) {
		segments = segments[1:]
	}

	return segments
}

// JoinSegments concatenates segment text.
func JoinSegments(segments []Segment) string {
	var builder strings.Builder
	for _, segment := range segments {
		builder.WriteString(segment.Text)
	}

	return builder.String()
}

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
	used := WriteCellsNoFill(screen, column, row, width, text, style)
	for used < width {
		screen.SetContent(column+used, row, ' ', nil, style)
		used++
	}

	return used
}

// WriteCellsNoFill writes as much text as fits in width cells without filling remaining cells.
func WriteCellsNoFill(screen ContentSetter, column, row, width int, text string, style tcell.Style) int {
	if row < 0 || column < 0 || width <= 0 {
		return 0
	}

	used := 0
	for _, segment := range Segments(text) {
		if used+segment.Width > width {
			break
		}

		used += WriteSegment(screen, column+used, row, width-used, segment, style)
	}

	return used
}

// WriteSegment writes one terminal grapheme segment.
func WriteSegment(
	screen ContentSetter,
	column int,
	row int,
	width int,
	segment Segment,
	style tcell.Style,
) int {
	if width <= 0 || segment.Width > width {
		return 0
	}

	if segment.Text == "\t" {
		return WriteTabSegment(screen, column, row, width, style)
	}

	if segment.Width <= 0 {
		return 0
	}

	runes := []rune(segment.Text)

	mainRune := ' '
	if len(runes) > 0 {
		mainRune = runes[0]
	}

	combining := []rune(nil)
	if len(runes) > 1 {
		combining = runes[1:]
	}

	screen.SetContent(column, row, mainRune, combining, style)

	for offset := 1; offset < segment.Width; offset++ {
		screen.SetContent(column+offset, row, 0, nil, style)
	}

	return segment.Width
}

// WriteTabSegment writes a tab as up to four spaces.
func WriteTabSegment(screen ContentSetter, column, row, width int, style tcell.Style) int {
	const tabWidth = 4

	spaces := min(tabWidth, width)
	for offset := range spaces {
		screen.SetContent(column+offset, row, ' ', nil, style)
	}

	return spaces
}
