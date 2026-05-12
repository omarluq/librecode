package terminal

import (
	"strings"
	"unicode"
)

type editorRender struct {
	Lines     []styledLine
	CursorCol int
	CursorRow int
}

func insertRuneAt(value []rune, cursor int, char rune) (next []rune, nextCursor int) {
	if char == 0 {
		return value, cursor
	}
	cursor = clampComposerCursor(cursor, len(value))
	next = append([]rune{}, value[:cursor]...)
	next = append(next, char)
	next = append(next, value[cursor:]...)

	return next, cursor + 1
}

func moveCursorLeft(value []rune, cursor int) int {
	cursor = clampComposerCursor(cursor, len(value))
	if cursor > 0 {
		return cursor - 1
	}

	return cursor
}

func moveCursorRight(value []rune, cursor int) int {
	cursor = clampComposerCursor(cursor, len(value))
	if cursor < len(value) {
		return cursor + 1
	}

	return cursor
}

func moveCursorLineStart(value []rune, cursor int) int {
	return currentLineStart(value, cursor)
}

func moveCursorLineEnd(value []rune, cursor int) int {
	return currentLineEnd(value, cursor)
}

func moveCursorWordLeft(value []rune, cursor int) int {
	return wordLeft(value, cursor)
}

func moveCursorWordRight(value []rune, cursor int) int {
	return wordRight(value, cursor)
}

func backspaceAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = clampComposerCursor(cursor, len(value))
	if cursor == 0 {
		return value, cursor
	}
	next = append([]rune{}, value[:cursor-1]...)
	next = append(next, value[cursor:]...)

	return next, cursor - 1
}

func deleteForwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = clampComposerCursor(cursor, len(value))
	if cursor >= len(value) {
		return value, cursor
	}
	next = append([]rune{}, value[:cursor]...)
	next = append(next, value[cursor+1:]...)

	return next, cursor
}

func deleteWordBackwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = clampComposerCursor(cursor, len(value))
	start := wordLeft(value, cursor)
	next = append([]rune{}, value[:start]...)
	next = append(next, value[cursor:]...)

	return next, start
}

func deleteWordForwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = clampComposerCursor(cursor, len(value))
	end := wordRight(value, cursor)
	next = append([]rune{}, value[:cursor]...)
	next = append(next, value[end:]...)

	return next, cursor
}

func deleteToLineStartAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = clampComposerCursor(cursor, len(value))
	start := currentLineStart(value, cursor)
	next = append([]rune{}, value[:start]...)
	next = append(next, value[cursor:]...)

	return next, start
}

func deleteToLineEndAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = clampComposerCursor(cursor, len(value))
	end := currentLineEnd(value, cursor)
	next = append([]rune{}, value[:cursor]...)
	next = append(next, value[end:]...)

	return next, cursor
}

func currentLineStart(value []rune, cursor int) int {
	cursor = clampComposerCursor(cursor, len(value))
	for index := cursor - 1; index >= 0; index-- {
		if value[index] == '\n' {
			return index + 1
		}
	}

	return 0
}

func currentLineEnd(value []rune, cursor int) int {
	cursor = clampComposerCursor(cursor, len(value))
	for index := cursor; index < len(value); index++ {
		if value[index] == '\n' {
			return index
		}
	}

	return len(value)
}

func renderEditor(
	value []rune,
	cursor int,
	width int,
	maxRows int,
	theme terminalTheme,
	border colorToken,
	label string,
) editorRender {
	innerWidth := max(1, width-4)
	bodyLines := editorBodyLines(value, innerWidth)
	cursorRow, cursorColumn := editorCursorPosition(value, cursor, innerWidth)
	visibleLines, skippedRows := visibleEditorLines(bodyLines, maxRows, cursorRow)
	lines := make([]styledLine, 0, len(visibleLines)+2)
	borderStyle := theme.style(border)
	bodyStyle := theme.style(colorText)
	lines = append(lines, newStyledLine(borderStyle, editorTopBorder(width, label)))
	for _, bodyLine := range visibleLines {
		bodyText := padRight(bodyLine, innerWidth)
		lines = append(lines, newStyledLine(bodyStyle, "│ "+bodyText+" │"))
	}
	lines = append(lines, newStyledLine(borderStyle, editorBottomBorder(width)))

	return editorRender{
		Lines:     lines,
		CursorCol: 2 + cursorColumn,
		CursorRow: 1 + cursorRow - skippedRows,
	}
}

func editorBodyLines(value []rune, width int) []string {
	if len(value) == 0 {
		return []string{""}
	}

	return editorWrapText(string(value), width)
}

func editorCursorPosition(value []rune, cursor, width int) (row, column int) {
	cursor = clampComposerCursor(cursor, len(value))
	prefix := string(value[:cursor])
	lines := editorWrapText(prefix, width)
	if len(lines) == 0 {
		return 0, 0
	}
	lastLine := lines[len(lines)-1]
	if strings.HasSuffix(prefix, "\n") {
		return len(lines) - 1, 0
	}

	return len(lines) - 1, terminalTextWidth(lastLine)
}

func editorWrapText(text string, width int) []string {
	if width <= 0 {
		return []string{""}
	}
	logicalLines := strings.Split(text, "\n")
	lines := make([]string, 0, len(logicalLines))
	for _, logicalLine := range logicalLines {
		lines = append(lines, editorWrapLogicalLine(logicalLine, width)...)
	}

	return lines
}

func editorWrapLogicalLine(line string, width int) []string {
	if line == "" {
		return []string{""}
	}

	segments := terminalTextSegments(line)
	lines := []string{}
	for len(segments) > 0 {
		breakIndex := terminalTextWrapBreakIndex(segments, width)
		lines = append(lines, terminalTextJoinSegments(segments[:breakIndex]))
		segments = segments[breakIndex:]
	}
	if len(lines) == 0 {
		lines = append(lines, "")
	}

	return lines
}

func visibleEditorLines(lines []string, maxRows, cursorRow int) (visible []string, skippedRows int) {
	if maxRows < 1 || len(lines) <= maxRows {
		return lines, 0
	}
	start := max(0, cursorRow-maxRows+1)
	if start+maxRows > len(lines) {
		start = len(lines) - maxRows
	}

	return lines[start : start+maxRows], start
}

func editorTopBorder(width int, label string) string {
	innerWidth := max(1, width-2)
	label = strings.TrimSpace(label)
	if label == "" {
		return "╭" + strings.Repeat("─", innerWidth) + "╮"
	}

	label = strings.ReplaceAll(label, "\n", " ")
	suffix := truncateText(label+"──", innerWidth)
	fillWidth := max(0, innerWidth-runeLen(suffix))

	return "╭" + strings.Repeat("─", fillWidth) + suffix + "╮"
}

func editorBottomBorder(width int) string {
	return "╰" + strings.Repeat("─", max(1, width-2)) + "╯"
}

func wordLeft(value []rune, cursor int) int {
	index := max(0, clampComposerCursor(cursor, len(value)))
	for index > 0 && unicode.IsSpace(value[index-1]) {
		index--
	}
	for index > 0 && !unicode.IsSpace(value[index-1]) {
		index--
	}

	return index
}

func wordRight(value []rune, cursor int) int {
	index := min(max(0, cursor), len(value))
	for index < len(value) && unicode.IsSpace(value[index]) {
		index++
	}
	for index < len(value) && !unicode.IsSpace(value[index]) {
		index++
	}

	return index
}
