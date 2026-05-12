package terminal

import (
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
)

type styledSpan struct {
	Style tcell.Style
	Text  string
}

type styledLine struct {
	Style tcell.Style
	Text  string
	Spans []styledSpan // optional per-segment styles for syntax-highlighted or mixed-style lines
}

func newStyledLine(style tcell.Style, text string) styledLine {
	return styledLine{Style: style, Text: text, Spans: nil}
}

func wrapText(text string, width int) []string {
	return terminalTextWrap(text, width)
}

func wrapTextPreserveWhitespace(text string, width int) []string {
	return terminalTextWrapPreserveWhitespace(text, width)
}

func truncateText(text string, width int) string {
	return terminalTextTruncate(text, width)
}

func padRight(text string, width int) string {
	return terminalTextPadRight(text, width)
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
