package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/tui"
)

func terminalKeyEvent(event *tcell.EventKey) extension.ComposerKeyEvent {
	if keyEvent, ok := tui.NewKeyEvent(event); ok {
		return extension.ComposerKeyEvent{
			Key:   keyEvent.Key,
			Text:  keyEvent.Text,
			Ctrl:  keyEvent.Ctrl,
			Alt:   keyEvent.Alt,
			Shift: keyEvent.Shift,
		}
	}

	keyName := strings.ToLower(event.Name())
	keyName = strings.ReplaceAll(keyName, "ctrl-", "ctrl+")
	keyName = strings.ReplaceAll(keyName, " ", "-")

	return extension.ComposerKeyEvent{
		Key:   keyName,
		Text:  event.Str(),
		Ctrl:  event.Modifiers()&tcell.ModCtrl != 0,
		Alt:   event.Modifiers()&tcell.ModAlt != 0,
		Shift: event.Modifiers()&tcell.ModShift != 0,
	}
}
