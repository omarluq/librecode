package tui

import (
	"strconv"
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
		if segment.Width == 0 {
			limit++
			continue
		}
		if used+segment.Width > width {
			break
		}

		used += segment.Width
		if segment.Text == " " || segment.Text == "\t" {
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

func trimLeadingSpaces(segments []Segment) []Segment {
	for len(segments) > 0 && (segments[0].Text == " " || segments[0].Text == "\t") {
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

// Int formats value in base 10.
func Int(value int) string {
	return strconv.Itoa(value)
}

// DrawText draws text clipped to rect width.
func DrawText(screen ContentSetter, rect Rect, style tcell.Style, text string) {
	if screen == nil || rect.Empty() {
		return
	}

	drawTextAt(screen, rect.X, rect.Y, rect.Width, style, text)
}

// DrawLine draws a styled line clipped to rect width.
func DrawLine(screen ContentSetter, rect Rect, line Line) {
	if screen == nil || rect.Empty() {
		return
	}
	if len(line.Spans) == 0 {
		drawTextAt(screen, rect.X, rect.Y, rect.Width, line.Style, line.Text)
		return
	}

	column := rect.X
	remaining := rect.Width
	for _, span := range line.Spans {
		if remaining <= 0 {
			break
		}
		drawn := drawTextAt(screen, column, rect.Y, remaining, span.Style, span.Text)
		column += drawn
		remaining -= drawn
	}
}

// DrawLines draws lines clipped to rect.
func DrawLines(screen ContentSetter, rect Rect, lines []Line) {
	if screen == nil || rect.Empty() {
		return
	}

	for row := 0; row < min(rect.Height, len(lines)); row++ {
		DrawLine(screen, Rect{X: rect.X, Y: rect.Y + row, Width: rect.Width, Height: 1}, lines[row])
	}
}

func drawTextAt(screen ContentSetter, x, y, width int, style tcell.Style, text string) int {
	used := 0
	for _, segment := range Segments(text) {
		if used+segment.Width > width {
			break
		}

		used += WriteSegment(screen, x+used, y, width-used, segment, style)
	}

	return used
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
	if screen == nil || row < 0 || column < 0 || width <= 0 {
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
func WriteSegment(screen ContentSetter, column, row, width int, segment Segment, style tcell.Style) int {
	if screen == nil || width <= 0 || segment.Width > width {
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

	var combining []rune
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
