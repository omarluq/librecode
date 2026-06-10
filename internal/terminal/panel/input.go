package panel

import (
	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/keyevent"
)

// ActionID identifies one app-level keybinding action relevant to panels.
type ActionID string

// Panel keybinding actions.
const (
	ActionSelectCancel   ActionID = "select_cancel"
	ActionSelectConfirm  ActionID = "select_confirm"
	ActionSelectUp       ActionID = "select_up"
	ActionSelectDown     ActionID = "select_down"
	ActionSelectPageUp   ActionID = "select_page_up"
	ActionSelectPageDown ActionID = "select_page_down"
)

// KeyMatcher matches app-level actions against key events.
type KeyMatcher interface {
	Matches(event *tcell.EventKey, action ActionID) bool
}

// ActionType is the result of handling one panel key event.
type ActionType string

// Panel action results.
const (
	ActionNone   ActionType = "none"
	ActionCancel ActionType = "cancel"
	ActionSelect ActionType = "select"
)

// Action is the result of handling one panel key event.
type Action struct {
	Type  ActionType
	Value string
}

// HandleKey mutates panel selection/query state for a key event and returns any selected action.
func (model *Model) HandleKey(event *tcell.EventKey, bindings KeyMatcher) Action {
	if bindings.Matches(event, ActionSelectCancel) {
		return Action{Type: ActionCancel, Value: ""}
	}
	if bindings.Matches(event, ActionSelectConfirm) {
		return model.selectedAction()
	}
	if bindings.Matches(event, ActionSelectUp) {
		model.MoveSelection(-1)
		return Action{Type: ActionNone, Value: ""}
	}
	if bindings.Matches(event, ActionSelectDown) {
		model.MoveSelection(1)
		return Action{Type: ActionNone, Value: ""}
	}
	if bindings.Matches(event, ActionSelectPageUp) {
		model.MoveSelection(-10)
		return Action{Type: ActionNone, Value: ""}
	}
	if bindings.Matches(event, ActionSelectPageDown) {
		model.MoveSelection(10)
		return Action{Type: ActionNone, Value: ""}
	}
	model.handleSearchKey(event)

	return Action{Type: ActionNone, Value: ""}
}

func (model *Model) selectedAction() Action {
	if value, ok := model.SelectedValue(); ok {
		return Action{Type: ActionSelect, Value: value}
	}

	return Action{Type: ActionNone, Value: ""}
}

func (model *Model) handleSearchKey(event *tcell.EventKey) {
	if model == nil || event == nil || !model.searchable {
		return
	}
	if event.Key() == tcell.KeyBackspace || event.Key() == tcell.KeyBackspace2 {
		model.BackspaceQuery()
		return
	}
	if event.Key() == tcell.KeyRune {
		model.AppendQueryRune(keyevent.Rune(event))
	}
}
