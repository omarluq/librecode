package terminal

import (
	"sort"
	"strings"
	"unicode"

	"github.com/gdamore/tcell/v3"
)

type actionID string

const (
	actionCursorUp                   actionID = "tui.editor.cursorUp"
	actionCursorDown                 actionID = "tui.editor.cursorDown"
	actionCursorLeft                 actionID = "tui.editor.cursorLeft"
	actionCursorRight                actionID = "tui.editor.cursorRight"
	actionCursorWordLeft             actionID = "tui.editor.cursorWordLeft"
	actionCursorWordRight            actionID = "tui.editor.cursorWordRight"
	actionCursorLineStart            actionID = "tui.editor.cursorLineStart"
	actionCursorLineEnd              actionID = "tui.editor.cursorLineEnd"
	actionDeleteCharBackward         actionID = "tui.editor.deleteCharBackward"
	actionDeleteCharForward          actionID = "tui.editor.deleteCharForward"
	actionDeleteWordBackward         actionID = "tui.editor.deleteWordBackward"
	actionDeleteWordForward          actionID = "tui.editor.deleteWordForward"
	actionDeleteToLineStart          actionID = "tui.editor.deleteToLineStart"
	actionDeleteToLineEnd            actionID = "tui.editor.deleteToLineEnd"
	actionInputNewLine               actionID = "tui.input.newLine"
	actionInputSubmit                actionID = "tui.input.submit"
	actionInputTab                   actionID = "tui.input.tab"
	actionSelectUp                   actionID = "tui.select.up"
	actionSelectDown                 actionID = "tui.select.down"
	actionSelectPageUp               actionID = "tui.select.pageUp"
	actionSelectPageDown             actionID = "tui.select.pageDown"
	actionSelectConfirm              actionID = "tui.select.confirm"
	actionSelectCancel               actionID = "tui.select.cancel"
	actionInterrupt                  actionID = "app.interrupt"
	actionForceExit                  actionID = "app.exit.force"
	actionThinkingCycle              actionID = "app.thinking.cycle"
	actionModelCycleForward          actionID = "app.model.cycleForward"
	actionModelCycleBackward         actionID = "app.model.cycleBackward"
	actionModelSelect                actionID = "app.model.select"
	actionToolsExpand                actionID = "app.tools.expand"
	actionThinkingToggle             actionID = "app.thinking.toggle"
	actionMessageFollowUp            actionID = "app.message.followUp"
	actionMessageDequeue             actionID = "app.message.dequeue"
	actionSessionToggleNamedFilter   actionID = "app.session.toggleNamedFilter"
	actionSessionNew                 actionID = "app.session.new"
	actionSessionTree                actionID = "app.session.tree"
	actionSessionFork                actionID = "app.session.fork"
	actionSessionResume              actionID = "app.session.resume"
	actionSessionTogglePath          actionID = "app.session.togglePath"
	actionSessionToggleSort          actionID = "app.session.toggleSort"
	actionSessionRename              actionID = "app.session.rename"
	actionSessionDelete              actionID = "app.session.delete"
	actionSessionDeleteNoninvasive   actionID = "app.session.deleteNoninvasive"
	actionTreeFoldOrUp               actionID = "app.tree.foldOrUp"
	actionTreeUnfoldOrDown           actionID = "app.tree.unfoldOrDown"
	actionTreeEditLabel              actionID = "app.tree.editLabel"
	actionTreeToggleLabelTimestamp   actionID = "app.tree.toggleLabelTimestamp"
	actionTreeFilterCycleForward     actionID = "app.tree.filter.cycleForward"
	actionTreeFilterCycleBackward    actionID = "app.tree.filter.cycleBackward"
	actionScopedModelsSave           actionID = "app.models.save"
	actionScopedModelsEnableAll      actionID = "app.models.enableAll"
	actionScopedModelsClearAll       actionID = "app.models.clearAll"
	actionScopedModelsToggleProvider actionID = "app.models.toggleProvider"
	actionScopedModelsReorderUp      actionID = "app.models.reorderUp"
	actionScopedModelsReorderDown    actionID = "app.models.reorderDown"
)

type keyBindingDefinition struct {
	description string
	keys        []string
}

type keybindings struct {
	definitions map[actionID]keyBindingDefinition
}

func newDefaultKeybindings() *keybindings {
	return &keybindings{definitions: defaultKeybindingDefinitions()}
}

func defaultKeybindingDefinitions() map[actionID]keyBindingDefinition {
	return map[actionID]keyBindingDefinition{
		actionCursorUp:                   binding("Move cursor up", "up"),
		actionCursorDown:                 binding("Move cursor down", "down"),
		actionCursorLeft:                 binding("Move cursor left", "left", "ctrl+b"),
		actionCursorRight:                binding("Move cursor right", "right", "ctrl+f"),
		actionCursorWordLeft:             binding("Move cursor word left", "alt+left", "ctrl+left", "alt+b"),
		actionCursorWordRight:            binding("Move cursor word right", "alt+right", "ctrl+right", "alt+f"),
		actionCursorLineStart:            binding("Move to line start", "home", "ctrl+a"),
		actionCursorLineEnd:              binding("Move to line end", "end", "ctrl+e"),
		actionDeleteCharBackward:         binding("Delete character backward", "backspace"),
		actionDeleteCharForward:          binding("Delete character forward", "delete"),
		actionDeleteWordBackward:         binding("Delete word backward", "ctrl+w", "alt+backspace"),
		actionDeleteWordForward:          binding("Delete word forward", "alt+d", "alt+delete"),
		actionDeleteToLineStart:          binding("Delete to line start", "ctrl+u"),
		actionDeleteToLineEnd:            binding("Delete to line end", "ctrl+k"),
		actionInputNewLine:               binding("Insert newline", "shift+enter"),
		actionInputSubmit:                binding("Submit input", "enter"),
		actionInputTab:                   binding("Autocomplete or toggle scope", "tab"),
		actionSelectUp:                   binding("Move selection up", "up"),
		actionSelectDown:                 binding("Move selection down", "down"),
		actionSelectPageUp:               binding("Page selection up", "pageUp"),
		actionSelectPageDown:             binding("Page selection down", "pageDown"),
		actionSelectConfirm:              binding("Confirm selection", "enter"),
		actionSelectCancel:               binding("Cancel selection", "escape"),
		actionInterrupt:                  binding("Cancel or abort", "escape"),
		actionForceExit:                  binding("Press twice to exit librecode", "ctrl+c"),
		actionThinkingCycle:              binding("Cycle thinking level", "shift+tab"),
		actionModelCycleForward:          binding("Cycle to next model", "ctrl+p"),
		actionModelCycleBackward:         binding("Cycle to previous model", "shift+ctrl+p"),
		actionModelSelect:                binding("Open model selector", "ctrl+l"),
		actionToolsExpand:                binding("Toggle tool output", "ctrl+o"),
		actionThinkingToggle:             binding("Toggle thinking blocks", "ctrl+t"),
		actionMessageFollowUp:            binding("Queue follow-up message", "alt+enter"),
		actionMessageDequeue:             binding("Restore queued messages", "alt+up"),
		actionSessionToggleNamedFilter:   binding("Toggle named session filter", "ctrl+n"),
		actionSessionNew:                 binding("Start a new session"),
		actionSessionTree:                binding("Open session tree"),
		actionSessionFork:                binding("Fork current session"),
		actionSessionResume:              binding("Resume a session"),
		actionSessionTogglePath:          binding("Toggle session path display", "ctrl+p"),
		actionSessionToggleSort:          binding("Toggle session sort mode", "ctrl+s"),
		actionSessionRename:              binding("Rename session", "ctrl+r"),
		actionSessionDelete:              binding("Delete session", "delete"),
		actionSessionDeleteNoninvasive:   binding("Delete session when query is empty", "ctrl+backspace"),
		actionTreeFoldOrUp:               binding("Fold tree branch or move up", "ctrl+left", "alt+left"),
		actionTreeUnfoldOrDown:           binding("Unfold tree branch or move down", "ctrl+right", "alt+right"),
		actionTreeEditLabel:              binding("Edit tree label", "shift+l"),
		actionTreeToggleLabelTimestamp:   binding("Toggle tree label timestamps", "shift+t"),
		actionTreeFilterCycleForward:     binding("Cycle tree filter forward", "ctrl+o"),
		actionTreeFilterCycleBackward:    binding("Cycle tree filter backward", "shift+ctrl+o"),
		actionScopedModelsSave:           binding("Save model selection", "ctrl+s"),
		actionScopedModelsEnableAll:      binding("Enable all models", "ctrl+a"),
		actionScopedModelsClearAll:       binding("Clear selected models", "ctrl+x"),
		actionScopedModelsToggleProvider: binding("Toggle provider models", "ctrl+p"),
		actionScopedModelsReorderUp:      binding("Move model up", "alt+up"),
		actionScopedModelsReorderDown:    binding("Move model down", "alt+down"),
	}
}

func binding(description string, keys ...string) keyBindingDefinition {
	return keyBindingDefinition{description: description, keys: keys}
}

func (bindings *keybindings) matches(event *tcell.EventKey, action actionID) bool {
	definition, ok := bindings.definitions[action]
	if !ok {
		return false
	}
	eventKeys := normalizedEventKeys(event)
	for _, configuredKey := range definition.keys {
		if _, ok := eventKeys[normalizeKeyName(configuredKey)]; ok {
			return true
		}
	}

	return false
}

func (bindings *keybindings) hint(action actionID) string {
	definition, ok := bindings.definitions[action]
	if !ok || len(definition.keys) == 0 {
		return "unbound"
	}

	return definition.keys[0]
}

func (bindings *keybindings) rows() []keyBindingRow {
	actions := make([]actionID, 0, len(bindings.definitions))
	for action := range bindings.definitions {
		actions = append(actions, action)
	}
	sort.Slice(actions, func(leftIndex, rightIndex int) bool {
		return actions[leftIndex] < actions[rightIndex]
	})
	rows := make([]keyBindingRow, 0, len(actions))
	for _, action := range actions {
		definition := bindings.definitions[action]
		rows = append(rows, keyBindingRow{
			Action:      string(action),
			Keys:        strings.Join(definition.keys, ", "),
			Description: definition.description,
		})
	}

	return rows
}

type keyBindingRow struct {
	Action      string
	Keys        string
	Description string
}

func normalizedEventKeys(event *tcell.EventKey) map[string]struct{} {
	keys := map[string]struct{}{}
	addKey(keys, eventKeyName(event))
	if event.Key() == tcell.KeyBacktab {
		addKey(keys, "shift+tab")
	}
	if event.Key() == tcell.KeyRune && unicode.IsUpper(eventRune(event)) {
		addKey(keys, "shift+"+strings.ToLower(string(eventRune(event))))
	}
	if event.Key() >= tcell.KeyCtrlA && event.Key() <= tcell.KeyCtrlZ {
		letter := rune('a' + event.Key() - tcell.KeyCtrlA)
		prefix := "ctrl+"
		if event.Modifiers()&tcell.ModShift != 0 {
			prefix = "shift+" + prefix
		}
		addKey(keys, prefix+string(letter))
	}

	return keys
}

func eventKeyName(event *tcell.EventKey) string {
	name := event.Name()
	if event.Key() == tcell.KeyRune {
		name = string(unicode.ToLower(eventRune(event)))
		if event.Modifiers()&tcell.ModAlt != 0 {
			name = "alt+" + name
		}
		if event.Modifiers()&tcell.ModCtrl != 0 {
			name = "ctrl+" + name
		}
	}

	return name
}

func addKey(keys map[string]struct{}, key string) {
	keys[normalizeKeyName(key)] = struct{}{}
}

func normalizeKeyName(key string) string {
	key = strings.TrimSpace(strings.ToLower(key))
	key = strings.ReplaceAll(key, "-", "+")
	key = strings.ReplaceAll(key, "pgup", "pageup")
	key = strings.ReplaceAll(key, "pgdn", "pagedown")
	key = strings.ReplaceAll(key, "esc", "escape")
	key = strings.ReplaceAll(key, "return", "enter")
	key = strings.ReplaceAll(key, "backtab", "shift+tab")

	return key
}
