package input

// Buffer is the editable terminal composer state.
type Buffer struct {
	Metadata map[string]any
	Text     string
	Label    string
	Chars    []string
	Cursor   int
}

// NewBuffer returns an initialized empty composer buffer.
func NewBuffer() Buffer {
	return Buffer{
		Metadata: map[string]any{},
		Text:     "",
		Label:    "",
		Chars:    []string{},
		Cursor:   0,
	}
}

// TextValue returns the current composer text.
func (buffer *Buffer) TextValue() string {
	return buffer.Text
}

// CursorValue returns the current rune cursor position.
func (buffer *Buffer) CursorValue() int {
	return buffer.Cursor
}

// Empty reports whether the composer text is empty.
func (buffer *Buffer) Empty() bool {
	return buffer.Text == ""
}

// SetRunes replaces the composer content and clamps the cursor.
func (buffer *Buffer) SetRunes(value []rune, cursor int) {
	text := string(value)
	buffer.Text = text
	buffer.Chars = Chars(value)
	buffer.Cursor = ClampCursor(cursor, len(value))
}

// SetText replaces the composer content and moves the cursor to the end.
func (buffer *Buffer) SetText(text string) {
	value := []rune(text)
	buffer.SetRunes(value, len(value))
}

// Clear empties the composer and returns the previous text.
func (buffer *Buffer) Clear() string {
	text := buffer.Text
	buffer.SetText("")

	return text
}

// Update applies a text/cursor mutation to the current composer state.
func (buffer *Buffer) Update(mutator func([]rune, int) ([]rune, int)) {
	value := []rune(buffer.Text)
	cursor := ClampCursor(buffer.Cursor, len(value))
	nextValue, nextCursor := mutator(value, cursor)
	buffer.SetRunes(nextValue, nextCursor)
}

// InsertRune inserts char at the cursor.
func (buffer *Buffer) InsertRune(char rune) {
	buffer.Update(func(value []rune, cursor int) ([]rune, int) {
		return InsertRuneAt(value, cursor, char)
	})
}

// MoveLeft moves the cursor left by one rune.
func (buffer *Buffer) MoveLeft() {
	buffer.Update(func(value []rune, cursor int) ([]rune, int) {
		return value, MoveCursorLeft(value, cursor)
	})
}

// MoveRight moves the cursor right by one rune.
func (buffer *Buffer) MoveRight() {
	buffer.Update(func(value []rune, cursor int) ([]rune, int) {
		return value, MoveCursorRight(value, cursor)
	})
}

// MoveWordLeft moves the cursor to the beginning of the previous word.
func (buffer *Buffer) MoveWordLeft() {
	buffer.Update(func(value []rune, cursor int) ([]rune, int) {
		return value, MoveCursorWordLeft(value, cursor)
	})
}

// MoveWordRight moves the cursor to the end of the next word.
func (buffer *Buffer) MoveWordRight() {
	buffer.Update(func(value []rune, cursor int) ([]rune, int) {
		return value, MoveCursorWordRight(value, cursor)
	})
}

// MoveLineStart moves the cursor to the start of the current line.
func (buffer *Buffer) MoveLineStart() {
	buffer.Update(func(value []rune, cursor int) ([]rune, int) {
		return value, MoveCursorLineStart(value, cursor)
	})
}

// MoveLineEnd moves the cursor to the end of the current line.
func (buffer *Buffer) MoveLineEnd() {
	buffer.Update(func(value []rune, cursor int) ([]rune, int) {
		return value, MoveCursorLineEnd(value, cursor)
	})
}

// DeleteBackward deletes one rune before the cursor.
func (buffer *Buffer) DeleteBackward() {
	buffer.Update(DeleteBackwardAt)
}

// DeleteForward deletes one rune at the cursor.
func (buffer *Buffer) DeleteForward() {
	buffer.Update(DeleteForwardAt)
}

// DeleteWordBackward deletes the previous word.
func (buffer *Buffer) DeleteWordBackward() {
	buffer.Update(DeleteWordBackwardAt)
}

// DeleteWordForward deletes the next word.
func (buffer *Buffer) DeleteWordForward() {
	buffer.Update(DeleteWordForwardAt)
}

// DeleteToLineStart deletes from the cursor to the start of the current line.
func (buffer *Buffer) DeleteToLineStart() {
	buffer.Update(DeleteToLineStartAt)
}

// DeleteToLineEnd deletes from the cursor to the end of the current line.
func (buffer *Buffer) DeleteToLineEnd() {
	buffer.Update(DeleteToLineEndAt)
}

// Chars converts runes into extension-visible string cells.
func Chars(value []rune) []string {
	chars := make([]string, 0, len(value))
	for _, char := range value {
		chars = append(chars, string(char))
	}

	return chars
}

// StringChars converts text into extension-visible string cells.
func StringChars(text string) []string {
	return Chars([]rune(text))
}

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
