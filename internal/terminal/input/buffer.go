package input

import "github.com/omarluq/librecode/tui"

// Buffer is the editable terminal composer state.
type Buffer = tui.TextArea

// NewBuffer returns an initialized empty composer buffer.
func NewBuffer() Buffer { return tui.NewTextArea() }

// Chars converts runes into extension-visible string cells.
func Chars(value []rune) []string { return tui.Chars(value) }

// StringChars converts text into extension-visible string cells.
func StringChars(text string) []string { return tui.StringChars(text) }

// ClampCursor clamps a rune cursor to the text length.
func ClampCursor(cursor, runeCount int) int { return tui.ClampCursor(cursor, runeCount) }
