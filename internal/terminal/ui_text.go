package terminal

import (
	"strings"

	"github.com/clipperhouse/displaywidth"
	"github.com/gdamore/tcell/v3"
)

type terminalTextSegment struct {
	Text  string
	Width int
}

func terminalTextWidth(text string) int {
	if text == "" {
		return 0
	}

	return displaywidth.String(text)
}

func terminalTextSegments(text string) []terminalTextSegment {
	segments := []terminalTextSegment{}
	iterator := displaywidth.StringGraphemes(text)
	for iterator.Next() {
		segments = append(segments, terminalTextSegment{
			Text:  iterator.Value(),
			Width: max(0, iterator.Width()),
		})
	}

	return segments
}

func terminalTextTruncate(text string, width int) string {
	if width <= 0 || text == "" {
		return ""
	}
	if terminalTextWidth(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}

	return terminalTextFit(text, width-1) + "…"
}

func terminalTextFit(text string, width int) string {
	if width <= 0 || text == "" {
		return ""
	}

	var builder strings.Builder
	used := 0
	for _, segment := range terminalTextSegments(text) {
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

func terminalTextPadRight(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = terminalTextFit(text, width)
	padding := width - terminalTextWidth(text)
	if padding <= 0 {
		return text
	}

	return text + strings.Repeat(" ", padding)
}

func terminalTextWrap(text string, width int) []string {
	return terminalTextWrapWithMode(text, width, false)
}

func terminalTextWrapPreserveWhitespace(text string, width int) []string {
	return terminalTextWrapWithMode(text, width, true)
}

func terminalTextWrapWithMode(text string, width int, preserveWhitespace bool) []string {
	if width <= 0 {
		return []string{""}
	}
	logicalLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(logicalLines))
	for _, logicalLine := range logicalLines {
		lines = append(lines, terminalTextWrapLogicalLineWithMode(logicalLine, width, preserveWhitespace)...)
	}

	return lines
}

func terminalTextWrapLogicalLineWithMode(line string, width int, preserveWhitespace bool) []string {
	if line == "" {
		return []string{""}
	}

	segments := terminalTextSegments(line)
	lines := []string{}
	for len(segments) > 0 {
		breakIndex := terminalTextWrapBreakIndex(segments, width)
		wrapped := terminalTextJoinSegments(segments[:breakIndex])
		if !preserveWhitespace {
			wrapped = strings.TrimRight(wrapped, " ")
		}
		lines = append(lines, wrapped)
		segments = segments[breakIndex:]
		if !preserveWhitespace {
			segments = terminalTextTrimLeadingSpaces(segments)
		}
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}

	return lines
}

func terminalTextWrapBreakIndex(segments []terminalTextSegment, width int) int {
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
		if terminalTextSegmentIsSpace(segment) {
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

func terminalTextSegmentIsSpace(segment terminalTextSegment) bool {
	return segment.Text == " " || segment.Text == "\t"
}

func terminalTextTrimLeadingSpaces(segments []terminalTextSegment) []terminalTextSegment {
	for len(segments) > 0 && terminalTextSegmentIsSpace(segments[0]) {
		segments = segments[1:]
	}

	return segments
}

func terminalTextJoinSegments(segments []terminalTextSegment) string {
	var builder strings.Builder
	for _, segment := range segments {
		builder.WriteString(segment.Text)
	}

	return builder.String()
}

func writeTextCells(screen cellTarget, column, row, width int, text string, style tcell.Style) int {
	used := writeTextCellsNoFill(screen, column, row, width, text, style)
	for used < width {
		screen.SetContent(column+used, row, ' ', nil, style)
		used++
	}

	return used
}

func writeTextCellsNoFill(screen cellTarget, column, row, width int, text string, style tcell.Style) int {
	if row < 0 || column < 0 || width <= 0 {
		return 0
	}
	used := 0
	for _, segment := range terminalTextSegments(text) {
		if used+segment.Width > width {
			break
		}
		used += writeTextSegment(screen, column+used, row, width-used, segment, style)
	}

	return used
}

func writeTextSegment(
	screen cellTarget,
	column int,
	row int,
	width int,
	segment terminalTextSegment,
	style tcell.Style,
) int {
	if width <= 0 || segment.Width > width {
		return 0
	}
	if segment.Text == "\t" {
		return writeTabSegment(screen, column, row, width, style)
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

func writeTabSegment(screen cellTarget, column, row, width int, style tcell.Style) int {
	const tabWidth = 4

	spaces := min(tabWidth, width)
	for offset := range spaces {
		screen.SetContent(column+offset, row, ' ', nil, style)
	}

	return spaces
}
