package tui

import (
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v3"
)

// TextArea is an editable multiline text buffer.
type TextArea struct {
	Metadata map[string]any
	Text     string
	Label    string
	Chars    []string
	Cursor   int
}

// NewTextArea returns an initialized empty text area.
func NewTextArea() TextArea {
	return TextArea{
		Metadata: map[string]any{},
		Text:     "",
		Label:    "",
		Chars:    []string{},
		Cursor:   0,
	}
}

// TextValue returns the current text.
func (area *TextArea) TextValue() string {
	if area == nil {
		return ""
	}

	return area.Text
}

// CursorValue returns the current rune cursor position.
func (area *TextArea) CursorValue() int {
	if area == nil {
		return 0
	}

	return area.Cursor
}

// Empty reports whether the text is empty.
func (area *TextArea) Empty() bool {
	return area == nil || area.Text == ""
}

// SetRunes replaces the content and clamps the cursor.
func (area *TextArea) SetRunes(value []rune, cursor int) {
	if area == nil {
		return
	}

	area.Text = string(value)
	area.Chars = Chars(value)
	area.Cursor = ClampCursor(cursor, len(value))
}

// SetText replaces the content and moves the cursor to the end.
func (area *TextArea) SetText(text string) {
	area.SetRunes([]rune(text), len([]rune(text)))
}

// Clear empties the text area and returns the previous text.
func (area *TextArea) Clear() string {
	if area == nil {
		return ""
	}

	text := area.Text
	area.SetText("")

	return text
}

// Update applies a text/cursor mutation to the current state.
func (area *TextArea) Update(mutator func([]rune, int) ([]rune, int)) {
	if area == nil || mutator == nil {
		return
	}

	value := []rune(area.Text)
	cursor := ClampCursor(area.Cursor, len(value))
	nextValue, nextCursor := mutator(value, cursor)
	area.SetRunes(nextValue, nextCursor)
}

// InsertRune inserts char at the cursor.
func (area *TextArea) InsertRune(char rune) {
	area.Update(func(value []rune, cursor int) ([]rune, int) { return InsertRuneAt(value, cursor, char) })
}

// MoveLeft moves the cursor left by one rune.
func (area *TextArea) MoveLeft() {
	area.Update(func(value []rune, cursor int) ([]rune, int) { return value, MoveCursorLeft(value, cursor) })
}

// MoveRight moves the cursor right by one rune.
func (area *TextArea) MoveRight() {
	area.Update(func(value []rune, cursor int) ([]rune, int) { return value, MoveCursorRight(value, cursor) })
}

// MoveWordLeft moves the cursor to the beginning of the previous word.
func (area *TextArea) MoveWordLeft() {
	area.Update(func(value []rune, cursor int) ([]rune, int) { return value, MoveCursorWordLeft(value, cursor) })
}

// MoveWordRight moves the cursor to the end of the next word.
func (area *TextArea) MoveWordRight() {
	area.Update(func(value []rune, cursor int) ([]rune, int) { return value, MoveCursorWordRight(value, cursor) })
}

// MoveLineStart moves the cursor to the start of the current line.
func (area *TextArea) MoveLineStart() {
	area.Update(func(value []rune, cursor int) ([]rune, int) { return value, MoveCursorLineStart(value, cursor) })
}

// MoveLineEnd moves the cursor to the end of the current line.
func (area *TextArea) MoveLineEnd() {
	area.Update(func(value []rune, cursor int) ([]rune, int) { return value, MoveCursorLineEnd(value, cursor) })
}

// DeleteBackward deletes one rune before the cursor.
func (area *TextArea) DeleteBackward() { area.Update(DeleteBackwardAt) }

// DeleteForward deletes one rune at the cursor.
func (area *TextArea) DeleteForward() { area.Update(DeleteForwardAt) }

// DeleteWordBackward deletes the previous word.
func (area *TextArea) DeleteWordBackward() { area.Update(DeleteWordBackwardAt) }

// DeleteWordForward deletes the next word.
func (area *TextArea) DeleteWordForward() { area.Update(DeleteWordForwardAt) }

// DeleteToLineStart deletes from the cursor to the start of the current line.
func (area *TextArea) DeleteToLineStart() { area.Update(DeleteToLineStartAt) }

// DeleteToLineEnd deletes from the cursor to the end of the current line.
func (area *TextArea) DeleteToLineEnd() { area.Update(DeleteToLineEndAt) }

// TextAreaRender describes rendered editor lines and cursor position.
type TextAreaRender struct {
	Lines     []Line
	CursorCol int
	CursorRow int
}

// TextAreaStyles configures text area rendering.
type TextAreaStyles struct {
	Border tcell.Style
	Body   tcell.Style
}

const (
	textAreaBorderPadding      = 4
	textAreaBorderRows         = 2
	textAreaCursorColumnOffset = 2
)

// Render renders this text area with a border.
func (area *TextArea) Render(width, maxRows int, styles TextAreaStyles) TextAreaRender {
	if area == nil {
		return renderTextArea(nil, 0, width, maxRows, styles, "")
	}

	return renderTextArea([]rune(area.Text), area.Cursor, width, maxRows, styles, area.Label)
}

func renderTextArea(value []rune, cursor, width, maxRows int, styles TextAreaStyles, label string) TextAreaRender {
	innerWidth := max(1, width-textAreaBorderPadding)
	bodyLines := TextAreaBodyLines(value, innerWidth)
	cursorRow, cursorColumn := TextAreaCursorPosition(value, cursor, innerWidth)
	visibleLines, skippedRows := VisibleLines(bodyLines, maxRows, cursorRow)
	lines := make([]Line, 0, len(visibleLines)+textAreaBorderRows)
	lines = append(lines, NewLine(styles.Border, TopBorder(width, label)))

	for _, bodyLine := range visibleLines {
		bodyText := PadRight(bodyLine, innerWidth)
		lines = append(lines, NewLine(styles.Body, "│ "+bodyText+" │"))
	}

	lines = append(lines, NewLine(styles.Border, BottomBorder(width)))

	return TextAreaRender{
		Lines:     lines,
		CursorCol: textAreaCursorColumnOffset + cursorColumn,
		CursorRow: 1 + cursorRow - skippedRows,
	}
}

// TextAreaBodyLines wraps the body text into display lines.
func TextAreaBodyLines(value []rune, width int) []string {
	if len(value) == 0 {
		return []string{""}
	}

	return WrapPreserveWhitespace(string(value), width)
}

// TextAreaCursorPosition returns the display row/column for cursor.
func TextAreaCursorPosition(value []rune, cursor, width int) (row, column int) {
	cursor = ClampCursor(cursor, len(value))
	prefix := string(value[:cursor])

	lines := WrapPreserveWhitespace(prefix, width)
	if len(lines) == 0 {
		return 0, 0
	}

	lastLine := lines[len(lines)-1]
	if strings.HasSuffix(prefix, "\n") {
		return len(lines) - 1, 0
	}

	return len(lines) - 1, Width(lastLine)
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

// Chars converts runes into string cells.
func Chars(value []rune) []string {
	chars := make([]string, 0, len(value))
	for _, char := range value {
		chars = append(chars, string(char))
	}

	return chars
}

// StringChars converts text into string cells.
func StringChars(text string) []string { return Chars([]rune(text)) }

// ClampCursor clamps a rune cursor to the text length.
func ClampCursor(cursor, runeCount int) int {
	if cursor < 0 {
		return 0
	}

	if cursor > runeCount {
		return runeCount
	}

	return cursor
}

// InsertRuneAt inserts char at cursor and returns the next value/cursor.
func InsertRuneAt(value []rune, cursor int, char rune) (next []rune, nextCursor int) {
	if char == 0 {
		return value, cursor
	}

	cursor = ClampCursor(cursor, len(value))
	next = append([]rune{}, value[:cursor]...)
	next = append(next, char)
	next = append(next, value[cursor:]...)

	return next, cursor + 1
}

// MoveCursorLeft moves the cursor one rune left.
func MoveCursorLeft(value []rune, cursor int) int {
	cursor = ClampCursor(cursor, len(value))
	if cursor > 0 {
		return cursor - 1
	}

	return cursor
}

// MoveCursorRight moves the cursor one rune right.
func MoveCursorRight(value []rune, cursor int) int {
	cursor = ClampCursor(cursor, len(value))
	if cursor < len(value) {
		return cursor + 1
	}

	return cursor
}

// MoveCursorLineStart moves the cursor to the current line start.
func MoveCursorLineStart(value []rune, cursor int) int { return CurrentLineStart(value, cursor) }

// MoveCursorLineEnd moves the cursor to the current line end.
func MoveCursorLineEnd(value []rune, cursor int) int { return CurrentLineEnd(value, cursor) }

// MoveCursorWordLeft moves the cursor to the start of the previous word.
func MoveCursorWordLeft(value []rune, cursor int) int { return WordLeft(value, cursor) }

// MoveCursorWordRight moves the cursor to the end of the next word.
func MoveCursorWordRight(value []rune, cursor int) int { return WordRight(value, cursor) }

// DeleteBackwardAt deletes the rune before cursor.
func DeleteBackwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	if cursor == 0 {
		return value, cursor
	}

	next = append([]rune{}, value[:cursor-1]...)
	next = append(next, value[cursor:]...)

	return next, cursor - 1
}

// DeleteForwardAt deletes the rune at cursor.
func DeleteForwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	if cursor >= len(value) {
		return value, cursor
	}

	next = append([]rune{}, value[:cursor]...)
	next = append(next, value[cursor+1:]...)

	return next, cursor
}

// DeleteWordBackwardAt deletes from cursor to the previous word boundary.
func DeleteWordBackwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	start := WordLeft(value, cursor)
	next = append([]rune{}, value[:start]...)
	next = append(next, value[cursor:]...)

	return next, start
}

// DeleteWordForwardAt deletes from cursor to the next word boundary.
func DeleteWordForwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	end := WordRight(value, cursor)
	next = append([]rune{}, value[:cursor]...)
	next = append(next, value[end:]...)

	return next, cursor
}

// DeleteToLineStartAt deletes from cursor to the current line start.
func DeleteToLineStartAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	start := CurrentLineStart(value, cursor)
	next = append([]rune{}, value[:start]...)
	next = append(next, value[cursor:]...)

	return next, start
}

// DeleteToLineEndAt deletes from cursor to the current line end.
func DeleteToLineEndAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	end := CurrentLineEnd(value, cursor)
	next = append([]rune{}, value[:cursor]...)
	next = append(next, value[end:]...)

	return next, cursor
}

// CurrentLineStart returns the rune index of the current line start.
func CurrentLineStart(value []rune, cursor int) int {
	cursor = ClampCursor(cursor, len(value))
	for index := cursor - 1; index >= 0; index-- {
		if value[index] == '\n' {
			return index + 1
		}
	}

	return 0
}

// CurrentLineEnd returns the rune index of the current line end.
func CurrentLineEnd(value []rune, cursor int) int {
	cursor = ClampCursor(cursor, len(value))
	for index := cursor; index < len(value); index++ {
		if value[index] == '\n' {
			return index
		}
	}

	return len(value)
}

// WordLeft returns the rune index at the beginning of the previous word.
func WordLeft(value []rune, cursor int) int {
	index := max(0, ClampCursor(cursor, len(value)))
	for index > 0 && unicode.IsSpace(value[index-1]) {
		index--
	}

	for index > 0 && !unicode.IsSpace(value[index-1]) {
		index--
	}

	return index
}

// WordRight returns the rune index at the end of the next word.
func WordRight(value []rune, cursor int) int {
	index := min(max(0, cursor), len(value))
	for index < len(value) && unicode.IsSpace(value[index]) {
		index++
	}

	for index < len(value) && !unicode.IsSpace(value[index]) {
		index++
	}

	return index
}
