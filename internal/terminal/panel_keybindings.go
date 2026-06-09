package terminal

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/panel"
)

type panelKeybindings struct {
	keys *keybindings
}

func (bindings panelKeybindings) Matches(event *tcell.EventKey, action panel.ActionID) bool {
	if bindings.keys == nil {
		return false
	}

	return bindings.keys.matches(event, panelActionID(action))
}

func panelActionID(action panel.ActionID) actionID {
	switch action {
	case panel.ActionSelectCancel:
		return actionSelectCancel
	case panel.ActionSelectConfirm:
		return actionSelectConfirm
	case panel.ActionSelectUp:
		return actionSelectUp
	case panel.ActionSelectDown:
		return actionSelectDown
	case panel.ActionSelectPageUp:
		return actionSelectPageUp
	case panel.ActionSelectPageDown:
		return actionSelectPageDown
	default:
		return actionID(action)
	}
}
