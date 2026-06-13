package tui

import (
	"strings"

	"github.com/gdamore/tcell/v3"
)

const keyCtrlR = "ctrl+r"

// KeyEvent is a normalized terminal key event.
type KeyEvent struct {
	Key   string
	Text  string
	Ctrl  bool
	Alt   bool
	Shift bool
}

// NewKeyEvent converts a tcell/v3 key event into a normalized key event.
func NewKeyEvent(event *tcell.EventKey) (KeyEvent, bool) {
	if event == nil {
		return emptyKeyEvent(), false
	}

	if event.Key() == tcell.KeyRune {
		return runeKeyEvent(event), true
	}

	key, ok := specialKeys()[event.Key()]
	if !ok {
		return emptyKeyEvent(), false
	}

	return KeyEvent{
		Key:   key,
		Text:  "",
		Ctrl:  strings.HasPrefix(key, "ctrl+"),
		Alt:   event.Modifiers()&tcell.ModAlt != 0,
		Shift: event.Modifiers()&tcell.ModShift != 0,
	}, true
}

func emptyKeyEvent() KeyEvent {
	return KeyEvent{Key: "", Text: "", Ctrl: false, Alt: false, Shift: false}
}

func runeKeyEvent(event *tcell.EventKey) KeyEvent {
	text := event.Str()
	ctrl := event.Modifiers()&tcell.ModCtrl != 0

	key := text
	if ctrl {
		key = "ctrl+" + strings.ToLower(text)
	}

	return KeyEvent{
		Key:   key,
		Text:  text,
		Ctrl:  ctrl,
		Alt:   event.Modifiers()&tcell.ModAlt != 0,
		Shift: event.Modifiers()&tcell.ModShift != 0,
	}
}

func specialKeys() map[tcell.Key]string {
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
		tcell.KeyCtrlR:      keyCtrlR,
		tcell.KeyCtrlU:      "ctrl+u",
		tcell.KeyCtrlW:      "ctrl+w",
	}
}
