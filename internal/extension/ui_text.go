package extension

import (
	"strings"

	"github.com/clipperhouse/displaywidth"
)

type uiTextSegment struct {
	Text  string
	Width int
}

func uiTextWidth(text string) int {
	if text == "" {
		return 0
	}

	return displaywidth.String(text)
}

func uiTextSegments(text string) []uiTextSegment {
	segments := []uiTextSegment{}
	iterator := displaywidth.StringGraphemes(text)
	for iterator.Next() {
		segments = append(segments, uiTextSegment{
			Text:  iterator.Value(),
			Width: max(0, iterator.Width()),
		})
	}

	return segments
}

func uiTextTruncate(text string, width int) string {
	if width <= 0 || text == "" {
		return ""
	}
	if uiTextWidth(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}

	return uiTextFit(text, width-1) + "…"
}

func uiTextFit(text string, width int) string {
	if width <= 0 || text == "" {
		return ""
	}

	var builder strings.Builder
	used := 0
	for _, segment := range uiTextSegments(text) {
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

func uiTextPadRight(text string, width int) string {
	if width <= 0 {
		return ""
	}
	text = uiTextFit(text, width)
	padding := width - uiTextWidth(text)
	if padding <= 0 {
		return text
	}

	return text + strings.Repeat(" ", padding)
}

func uiTextWrap(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	logicalLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(logicalLines))
	for _, logicalLine := range logicalLines {
		lines = append(lines, uiTextWrapLogicalLine(logicalLine, width)...)
	}

	return lines
}

func uiTextWrapLogicalLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}

	segments := uiTextSegments(line)
	lines := []string{}
	for len(segments) > 0 {
		breakIndex := uiTextWrapBreakIndex(segments, width)
		lines = append(lines, strings.TrimRight(uiTextJoinSegments(segments[:breakIndex]), " "))
		segments = uiTextTrimLeadingSpaces(segments[breakIndex:])
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}

	return lines
}

func uiTextWrapBreakIndex(segments []uiTextSegment, width int) int {
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

func uiTextViewport(lines []string, height, offset int) (visible []string, start, end, maxOffset int) {
	if height <= 0 || len(lines) == 0 {
		return []string{}, 0, 0, 0
	}
	maxOffset = max(0, len(lines)-height)
	offset = clampInt(offset, 0, maxOffset)
	end = len(lines) - offset
	start = max(0, end-height)

	return append([]string{}, lines[start:end]...), start, end, maxOffset
}

func uiTextTrimLeadingSpaces(segments []uiTextSegment) []uiTextSegment {
	for len(segments) > 0 && (segments[0].Text == " " || segments[0].Text == "\t") {
		segments = segments[1:]
	}

	return segments
}

func uiTextJoinSegments(segments []uiTextSegment) string {
	var builder strings.Builder
	for _, segment := range segments {
		builder.WriteString(segment.Text)
	}

	return builder.String()
}
