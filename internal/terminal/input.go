package terminal

import (
	"context"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tui"
)

func (app *App) handleEvent(ctx context.Context, event tcell.Event) (bool, error) {
	switch typedEvent := event.(type) {
	case *tcell.EventResize:
		return false, app.applyResizeEvent(ctx, typedEvent)
	case *tcell.EventKey:
		return app.handleKey(ctx, typedEvent)
	case *tcell.EventMouse:
		app.handleMouse(typedEvent)

		return false, nil
	case *tcell.EventInterrupt:
		return app.handleInterrupt(ctx, typedEvent)
	default:
		return false, nil
	}
}

func (app *App) applyResizeEvent(ctx context.Context, event *tcell.EventResize) error {
	app.lastResize = event

	return app.handleResizeExtensions(ctx)
}

func (app *App) handleKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	result := app.handlePriorityKey(ctx, event)
	if result.handled || result.err != nil {
		return result.shouldQuit, result.err
	}

	return app.handleInputKey(ctx, event)
}

type keyHandlingResult struct {
	err        error
	shouldQuit bool
	handled    bool
}

func (app *App) handlePriorityKey(ctx context.Context, event *tcell.EventKey) keyHandlingResult {
	if app.handleWorkingInterruptKey(ctx, event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	if app.keys.matches(event, actionForceExit) && app.composerBuffer.Empty() {
		return keyHandlingResult{err: nil, shouldQuit: app.handleForceExit(), handled: true}
	}

	if result := app.handlePanelPriorityKey(ctx, event); result.handled || result.err != nil {
		return result
	}

	if app.handleAutocompletePriorityKey(event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	if handled, err := app.handleExtensionKey(ctx, event); handled || err != nil {
		return keyHandlingResult{err: err, shouldQuit: false, handled: true}
	}

	if app.handlePreEditorKey(ctx, event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
}

func (app *App) handlePanelPriorityKey(ctx context.Context, event *tcell.EventKey) keyHandlingResult {
	if app.mode != modePanel || app.panel == nil {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
	}

	return keyHandlingResult{err: app.handlePanelKey(ctx, event), shouldQuit: false, handled: true}
}

func (app *App) handleAutocompletePriorityKey(event *tcell.EventKey) bool {
	return app.handleAutocompleteEscape(event) || app.handleFocusedAutocompleteKey(event)
}

func (app *App) handleAutocompleteEscape(event *tcell.EventKey) bool {
	if app.busy() || !app.autocompleteActive() || event.Key() != tcell.KeyEscape {
		return false
	}

	app.closeAutocomplete()

	return true
}

func (app *App) handleFocusedAutocompleteKey(event *tcell.EventKey) bool {
	if app.busy() || !app.autocompleteActive() {
		return false
	}

	return app.handleAutocompleteKey(event)
}

func (app *App) handleInputKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if app.keys.matches(event, actionInputClear) && !app.composerBuffer.Empty() {
		app.composerBuffer.Clear()
		app.resetPromptHistoryNavigation()
		app.resetAutocompleteSelection()
		app.escapePresses = 0

		return false, nil
	}

	if app.keys.matches(event, actionInputSubmit) {
		return app.submit(ctx)
	}

	if app.keys.matches(event, actionInputNewLine) {
		app.resetPromptHistoryNavigation()
		app.resetAutocompleteSelection()
		app.escapePresses = 0
		app.composerBuffer.InsertRune('\n')

		return false, nil
	}

	if app.keys.matches(event, actionInputTab) && app.acceptAutocomplete() {
		return false, nil
	}

	app.handleEditorKey(event)

	return false, nil
}

func (app *App) handlePreEditorKey(ctx context.Context, event *tcell.EventKey) bool {
	if app.handleTranscriptScroll(event) {
		return true
	}

	if app.handleGlobalShortcut(ctx, event) || app.handlePromptHistoryKey(event) {
		return true
	}

	return false
}

func (app *App) handleGlobalShortcut(ctx context.Context, event *tcell.EventKey) bool {
	shortcuts := app.globalShortcuts(ctx)
	for _, shortcut := range shortcuts {
		if app.keys.matches(event, shortcut.action) {
			shortcut.handler()

			return true
		}
	}

	return false
}

func (app *App) globalShortcuts(ctx context.Context) []shortcutHandler {
	return []shortcutHandler{
		{action: actionInterrupt, handler: func() { app.handleEscape(ctx) }},
		{action: actionModelSelect, handler: app.openModelPanel},
		{action: actionThinkingCycle, handler: app.cycleThinking},
		{action: actionModelCycleForward, handler: func() { app.cycleModel(1) }},
		{action: actionModelCycleBackward, handler: func() { app.cycleModel(-1) }},
		{action: actionToolsExpand, handler: app.toggleToolsExpanded},
		{action: actionThinkingToggle, handler: app.toggleThinkingHidden},
		{action: actionMessageFollowUp, handler: app.queueFollowUp},
		{action: actionMessageDequeue, handler: app.dequeueFollowUp},
	}
}

type shortcutHandler struct {
	handler func()
	action  actionID
}

func (app *App) handleEditorKey(event *tcell.EventKey) {
	app.escapePresses = 0

	actions := app.editorActions()
	for _, action := range actions {
		if app.keys.matches(event, action.action) {
			app.resetPromptHistoryNavigation()
			app.resetAutocompleteSelection()
			action.handler()

			return
		}
	}

	if event.Key() == tcell.KeyRune {
		app.resetPromptHistoryNavigation()
		app.resetAutocompleteSelection()
		app.composerBuffer.InsertRune(tui.EventRune(event))
	}
}

func (app *App) editorActions() []shortcutHandler {
	return []shortcutHandler{
		app.composerShortcut(actionCursorLeft, app.composerBuffer.MoveLeft),
		app.composerShortcut(actionCursorRight, app.composerBuffer.MoveRight),
		app.composerShortcut(actionCursorWordLeft, app.composerBuffer.MoveWordLeft),
		app.composerShortcut(actionCursorWordRight, app.composerBuffer.MoveWordRight),
		app.composerShortcut(actionCursorLineStart, app.composerBuffer.MoveLineStart),
		app.composerShortcut(actionCursorLineEnd, app.composerBuffer.MoveLineEnd),
		app.composerShortcut(actionDeleteCharBackward, app.composerBuffer.DeleteBackward),
		app.composerShortcut(actionDeleteCharForward, app.composerBuffer.DeleteForward),
		app.composerShortcut(actionDeleteWordBackward, app.composerBuffer.DeleteWordBackward),
		app.composerShortcut(actionDeleteWordForward, app.composerBuffer.DeleteWordForward),
		app.composerShortcut(actionDeleteToLineStart, app.composerBuffer.DeleteToLineStart),
		app.composerShortcut(actionDeleteToLineEnd, app.composerBuffer.DeleteToLineEnd),
	}
}

func (app *App) composerShortcut(action actionID, handler func()) shortcutHandler {
	return shortcutHandler{action: action, handler: handler}
}

func (app *App) handlePanelKey(ctx context.Context, event *tcell.EventKey) error {
	if event.Key() == tcell.KeyEscape {
		app.closePanel()

		return nil
	}

	if app.selectedPanelKind == panelSessions && app.handleSessionPanelKey(ctx, event) {
		return nil
	}

	if app.selectedPanelKind == panelScopedModels && app.handleScopedModelKey(event) {
		return nil
	}

	action := app.panel.HandleKey(event, panelKeybindings{keys: app.keys})
	switch action.Type {
	case panel.ActionCancel:
		app.closePanel()
	case panel.ActionSelect:
		return app.applyPanelSelection(ctx, action.Value)
	case panel.ActionNone:
		return nil
	}

	return nil
}
