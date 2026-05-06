package terminal

import (
	"strings"
	"unicode"
)

type editor struct {
	value  []rune
	cursor int
}

type editorRender struct {
	Lines     []styledLine
	CursorCol int
	CursorRow int
}

func newEditor() *editor {
	return &editor{value: []rune{}, cursor: 0}
}

func (input *editor) text() string {
	return string(input.value)
}

func (input *editor) empty() bool {
	return len(input.value) == 0
}

func (input *editor) setText(text string) {
	input.value = []rune(text)
	input.cursor = len(input.value)
}

func (input *editor) clear() string {
	text := input.text()
	input.value = []rune{}
	input.cursor = 0

	return text
}

func (input *editor) insertRune(value rune) {
	if value == 0 {
		return
	}
	input.value = append(input.value[:input.cursor], append([]rune{value}, input.value[input.cursor:]...)...)
	input.cursor++
}

func (input *editor) moveLeft() {
	if input.cursor > 0 {
		input.cursor--
	}
}

func (input *editor) moveRight() {
	if input.cursor < len(input.value) {
		input.cursor++
	}
}

func (input *editor) moveLineStart() {
	input.cursor = input.currentLineStart()
}

func (input *editor) moveLineEnd() {
	input.cursor = input.currentLineEnd()
}

func (input *editor) moveWordLeft() {
	input.cursor = wordLeft(input.value, input.cursor)
}

func (input *editor) moveWordRight() {
	input.cursor = wordRight(input.value, input.cursor)
}

func (input *editor) backspace() {
	if input.cursor == 0 {
		return
	}
	input.value = append(input.value[:input.cursor-1], input.value[input.cursor:]...)
	input.cursor--
}

func (input *editor) deleteForward() {
	if input.cursor >= len(input.value) {
		return
	}
	input.value = append(input.value[:input.cursor], input.value[input.cursor+1:]...)
}

func (input *editor) deleteWordBackward() {
	start := wordLeft(input.value, input.cursor)
	input.value = append(input.value[:start], input.value[input.cursor:]...)
	input.cursor = start
}

func (input *editor) deleteWordForward() {
	end := wordRight(input.value, input.cursor)
	input.value = append(input.value[:input.cursor], input.value[end:]...)
}

func (input *editor) deleteToLineStart() {
	start := input.currentLineStart()
	input.value = append(input.value[:start], input.value[input.cursor:]...)
	input.cursor = start
}

func (input *editor) deleteToLineEnd() {
	end := input.currentLineEnd()
	input.value = append(input.value[:input.cursor], input.value[end:]...)
}

func (input *editor) currentLineStart() int {
	for index := input.cursor - 1; index >= 0; index-- {
		if input.value[index] == '\n' {
			return index + 1
		}
	}

	return 0
}

func (input *editor) currentLineEnd() int {
	for index := input.cursor; index < len(input.value); index++ {
		if input.value[index] == '\n' {
			return index
		}
	}

	return len(input.value)
}

func (input *editor) render(width, maxRows int, theme terminalTheme, border colorToken) editorRender {
	innerWidth := max(1, width-4)
	bodyLines := input.bodyLines(innerWidth)
	cursorRow, cursorColumn := input.cursorPosition(innerWidth)
	visibleLines, skippedRows := visibleEditorLines(bodyLines, maxRows, cursorRow)
	lines := make([]styledLine, 0, len(visibleLines)+2)
	borderStyle := theme.style(border)
	bodyStyle := theme.style(colorText)
	lines = append(lines, styledLine{Style: borderStyle, Text: editorTopBorder(width)})
	for _, bodyLine := range visibleLines {
		text := "│ " + padRight(bodyLine, innerWidth) + " │"
		lines = append(lines, styledLine{Style: bodyStyle, Text: text})
	}
	lines = append(lines, styledLine{Style: borderStyle, Text: editorBottomBorder(width)})

	return editorRender{
		Lines:     lines,
		CursorCol: 2 + cursorColumn,
		CursorRow: 1 + cursorRow - skippedRows,
	}
}

func (input *editor) bodyLines(width int) []string {
	if len(input.value) == 0 {
		return []string{""}
	}

	return wrapText(input.text(), width)
}

func (input *editor) cursorPosition(width int) (row, column int) {
	prefix := string(input.value[:input.cursor])
	lines := wrapText(prefix, width)
	if len(lines) == 0 {
		return 0, 0
	}
	lastLine := lines[len(lines)-1]
	if strings.HasSuffix(prefix, "\n") {
		return len(lines) - 1, 0
	}

	return len(lines) - 1, runeLen(lastLine)
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

func editorTopBorder(width int) string {
	return "╭" + strings.Repeat("─", max(1, width-2)) + "╮"
}

func editorBottomBorder(width int) string {
	return "╰" + strings.Repeat("─", max(1, width-2)) + "╯"
}

func wordLeft(value []rune, cursor int) int {
	index := max(0, cursor)
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
