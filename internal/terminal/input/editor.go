package input

import "unicode"

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

// MoveCursorLeft moves cursor left by one rune.
func MoveCursorLeft(value []rune, cursor int) int {
	cursor = ClampCursor(cursor, len(value))
	if cursor > 0 {
		return cursor - 1
	}

	return cursor
}

// MoveCursorRight moves cursor right by one rune.
func MoveCursorRight(value []rune, cursor int) int {
	cursor = ClampCursor(cursor, len(value))
	if cursor < len(value) {
		return cursor + 1
	}

	return cursor
}

// MoveCursorLineStart moves cursor to the current line start.
func MoveCursorLineStart(value []rune, cursor int) int {
	return CurrentLineStart(value, cursor)
}

// MoveCursorLineEnd moves cursor to the current line end.
func MoveCursorLineEnd(value []rune, cursor int) int {
	return CurrentLineEnd(value, cursor)
}

// MoveCursorWordLeft moves cursor to the beginning of the previous word.
func MoveCursorWordLeft(value []rune, cursor int) int {
	return WordLeft(value, cursor)
}

// MoveCursorWordRight moves cursor to the end of the next word.
func MoveCursorWordRight(value []rune, cursor int) int {
	return WordRight(value, cursor)
}

// DeleteBackwardAt deletes one rune before the cursor.
func DeleteBackwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	if cursor == 0 {
		return value, cursor
	}

	next = append([]rune{}, value[:cursor-1]...)
	next = append(next, value[cursor:]...)

	return next, cursor - 1
}

// DeleteForwardAt deletes one rune at the cursor.
func DeleteForwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	if cursor >= len(value) {
		return value, cursor
	}

	next = append([]rune{}, value[:cursor]...)
	next = append(next, value[cursor+1:]...)

	return next, cursor
}

// DeleteWordBackwardAt deletes the previous word.
func DeleteWordBackwardAt(value []rune, cursor int) (next []rune, nextCursor int) {
	cursor = ClampCursor(cursor, len(value))
	start := WordLeft(value, cursor)
	next = append([]rune{}, value[:start]...)
	next = append(next, value[cursor:]...)

	return next, start
}

// DeleteWordForwardAt deletes the next word.
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
