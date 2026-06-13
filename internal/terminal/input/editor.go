package input

import "github.com/omarluq/librecode/tui"

// InsertRuneAt inserts char at cursor and returns the next value/cursor.
func InsertRuneAt(value []rune, cursor int, char rune) (next []rune, nextCursor int) {
	return tui.InsertRuneAt(value, cursor, char)
}

// MoveCursorLeft moves cursor left by one rune.
func MoveCursorLeft(value []rune, cursor int) int { return tui.MoveCursorLeft(value, cursor) }

// MoveCursorRight moves cursor right by one rune.
func MoveCursorRight(value []rune, cursor int) int { return tui.MoveCursorRight(value, cursor) }

// MoveCursorLineStart moves cursor to the current line start.
func MoveCursorLineStart(value []rune, cursor int) int { return tui.MoveCursorLineStart(value, cursor) }

// MoveCursorLineEnd moves cursor to the current line end.
func MoveCursorLineEnd(value []rune, cursor int) int { return tui.MoveCursorLineEnd(value, cursor) }

// MoveCursorWordLeft moves cursor to the beginning of the previous word.
func MoveCursorWordLeft(value []rune, cursor int) int { return tui.MoveCursorWordLeft(value, cursor) }

// MoveCursorWordRight moves cursor to the end of the next word.
func MoveCursorWordRight(value []rune, cursor int) int { return tui.MoveCursorWordRight(value, cursor) }

// DeleteBackwardAt deletes one rune before the cursor.
func DeleteBackwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	return tui.DeleteBackwardAt(value, cursor)
}

// DeleteForwardAt deletes one rune at the cursor.
func DeleteForwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	return tui.DeleteForwardAt(value, cursor)
}

// DeleteWordBackwardAt deletes the previous word.
func DeleteWordBackwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	return tui.DeleteWordBackwardAt(value, cursor)
}

// DeleteWordForwardAt deletes the next word.
func DeleteWordForwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	return tui.DeleteWordForwardAt(value, cursor)
}

// DeleteToLineStartAt deletes from cursor to the current line start.
func DeleteToLineStartAt(value []rune, cursor int) (next []rune, nextCursor int) {
	return tui.DeleteToLineStartAt(value, cursor)
}

// DeleteToLineEndAt deletes from cursor to the current line end.
func DeleteToLineEndAt(value []rune, cursor int) (next []rune, nextCursor int) {
	return tui.DeleteToLineEndAt(value, cursor)
}

// CurrentLineStart returns the rune index of the current line start.
func CurrentLineStart(value []rune, cursor int) int { return tui.CurrentLineStart(value, cursor) }

// CurrentLineEnd returns the rune index of the current line end.
func CurrentLineEnd(value []rune, cursor int) int { return tui.CurrentLineEnd(value, cursor) }

// WordLeft returns the rune index at the beginning of the previous word.
func WordLeft(value []rune, cursor int) int { return tui.WordLeft(value, cursor) }

// WordRight returns the rune index at the end of the next word.
func WordRight(value []rune, cursor int) int { return tui.WordRight(value, cursor) }
