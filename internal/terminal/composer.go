package terminal

import (
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/extension"
)

const composerKeyCtrlR = "ctrl+r"

func editorChars(value []rune) []string {
	chars := make([]string, 0, len(value))
	for _, char := range value {
		chars = append(chars, string(char))
	}

	return chars
}

func composerKeyEvent(event *tcell.EventKey) (extension.ComposerKeyEvent, bool) {
	if event.Key() == tcell.KeyRune {
		return composerRuneKeyEvent(event), true
	}

	key, ok := composerSpecialKeys()[event.Key()]
	if !ok {
		var keyEvent extension.ComposerKeyEvent

		return keyEvent, false
	}

	return extension.ComposerKeyEvent{
		Key:   key,
		Text:  "",
		Ctrl:  strings.HasPrefix(key, "ctrl+"),
		Alt:   event.Modifiers()&tcell.ModAlt != 0,
		Shift: event.Modifiers()&tcell.ModShift != 0,
	}, true
}

func composerRuneKeyEvent(event *tcell.EventKey) extension.ComposerKeyEvent {
	text := event.Str()
	ctrl := event.Modifiers()&tcell.ModCtrl != 0
	key := text
	if ctrl {
		key = "ctrl+" + strings.ToLower(text)
	}

	return extension.ComposerKeyEvent{
		Key:   key,
		Text:  text,
		Ctrl:  ctrl,
		Alt:   event.Modifiers()&tcell.ModAlt != 0,
		Shift: event.Modifiers()&tcell.ModShift != 0,
	}
}

func composerSpecialKeys() map[tcell.Key]string {
	return map[tcell.Key]string{
		tcell.KeyEscape:     "escape",
		tcell.KeyEnter:      "enter",
		tcell.KeyTab:        "tab",
		tcell.KeyBacktab:    "shift+tab",
		tcell.KeyBackspace:  "backspace",
		tcell.KeyBackspace2: "backspace",
		tcell.KeyDelete:     "delete",
		tcell.KeyLeft:       "left",
		tcell.KeyRight:      "right",
		tcell.KeyUp:         "up",
		tcell.KeyDown:       "down",
		tcell.KeyHome:       "home",
		tcell.KeyEnd:        "end",
		tcell.KeyCtrlA:      "ctrl+a",
		tcell.KeyCtrlB:      "ctrl+b",
		tcell.KeyCtrlC:      "ctrl+c",
		tcell.KeyCtrlE:      "ctrl+e",
		tcell.KeyCtrlF:      "ctrl+f",
		tcell.KeyCtrlK:      "ctrl+k",
		tcell.KeyCtrlR:      composerKeyCtrlR,
		tcell.KeyCtrlU:      "ctrl+u",
		tcell.KeyCtrlW:      "ctrl+w",
	}
}
