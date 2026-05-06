package terminal

import (
	"unicode/utf8"

	"github.com/gdamore/tcell/v3"
)

func eventRune(event *tcell.EventKey) rune {
	value, _ := utf8.DecodeRuneInString(event.Str())

	return value
}
