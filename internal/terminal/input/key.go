package input

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tui"
)

// ComposerKeyEvent converts a terminal key event into an extension composer key event.
func ComposerKeyEvent(event *tcell.EventKey) (extension.ComposerKeyEvent, bool) {
	keyEvent, ok := tui.NewKeyEvent(event)
	if !ok {
		return extension.ComposerKeyEvent{
			Key:   "",
			Text:  "",
			Ctrl:  false,
			Alt:   false,
			Shift: false,
		}, false
	}

	return extension.ComposerKeyEvent{
		Key:   keyEvent.Key,
		Text:  keyEvent.Text,
		Ctrl:  keyEvent.Ctrl,
		Alt:   keyEvent.Alt,
		Shift: keyEvent.Shift,
	}, true
}
