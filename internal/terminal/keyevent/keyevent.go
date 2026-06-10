// Package keyevent provides small helpers for tcell key events.
package keyevent

import (
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
)

// Rune returns the first rune carried by a tcell rune key event.
func Rune(event *tcell.EventKey) rune {
	if event == nil || event.Str() == "" {
		return 0
	}
	value, _ := utf8.DecodeRuneInString(event.Str())

	return value
}
