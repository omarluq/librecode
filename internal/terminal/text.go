package terminal

import (
	"strings"
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
)

type styledLine struct {
	Style tcell.Style
	Text  string
}

func wrapText(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	logicalLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(logicalLines))
	for _, logicalLine := range logicalLines {
		lines = append(lines, wrapLogicalLine(logicalLine, width)...)
	}

	return lines
}

func wrapLogicalLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}
	runes := []rune(line)
	lines := make([]string, 0, len(runes)/width+1)
	for len(runes) > width {
		breakIndex := wrapBreakIndex(runes, width)
		lines = append(lines, strings.TrimRight(string(runes[:breakIndex]), " "))
		runes = trimLeadingSpaces(runes[breakIndex:])
	}
	lines = append(lines, string(runes))

	return lines
}

func wrapBreakIndex(runes []rune, width int) int {
	limit := min(width, len(runes))
	for index := limit - 1; index > 0; index-- {
		if runes[index] == ' ' || runes[index] == '\t' {
			return index + 1
		}
	}

	return limit
}

func trimLeadingSpaces(runes []rune) []rune {
	for len(runes) > 0 && (runes[0] == ' ' || runes[0] == '\t') {
		runes = runes[1:]
	}

	return runes
}

func truncateText(text string, width int) string {
	if width <= 0 {
		return ""
	}
	if runeLen(text) <= width {
		return text
	}
	if width == 1 {
		return "…"
	}
	runes := []rune(text)

	return string(runes[:width-1]) + "…"
}

func padRight(text string, width int) string {
	length := runeLen(text)
	if length >= width {
		return truncateText(text, width)
	}

	return text + strings.Repeat(" ", width-length)
}

func runeLen(text string) int {
	return utf8.RuneCountInString(text)
}

func safeSlice[T any](items []T, maxItems int) []T {
	if maxItems < 0 || len(items) <= maxItems {
		return items
	}

	return items[len(items)-maxItems:]
}
