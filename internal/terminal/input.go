package terminal

import (
	"context"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/terminal/panel"
	"github.com/omarluq/librecode/internal/tui"
)

const readOnlyAgentInspectionStatus = "agent task inspection is read-only while the parent response runs"

func (app *App) handleEvent(ctx context.Context, event tcell.Event) (bool, error) {
	switch typedEvent := event.(type) {
	case *tcell.EventResize:
		return false, app.applyResizeEvent(ctx, typedEvent)
	case *tcell.EventPaste:
		if app.inspectingWhilePromptRuns() {
			app.bracketedPaste = false
			app.setStatus(readOnlyAgentInspectionStatus)

			return false, nil
		}

		app.bracketedPaste = typedEvent.Start()

		return false, nil
	case *tcell.EventKey:
		return app.handleKeyEvent(ctx, typedEvent)
	case *tcell.EventMouse:
		app.handleMouse(typedEvent)

		return false, nil
	case *tcell.EventInterrupt:
		return app.handleInterrupt(ctx, typedEvent)
	default:
		return false, nil
	}
}

func (app *App) handleKeyEvent(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if !event.Pressed() {
		return false, nil
	}

	if app.bracketedPaste {
		if app.inspectingWhilePromptRuns() {
			app.bracketedPaste = false
			app.setStatus(readOnlyAgentInspectionStatus)

			return false, nil
		}

		app.insertPastedKey(event)

		return false, nil
	}

	return app.handleKey(ctx, event)
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
	if result := app.handleInspectionAndModalPriorityKey(ctx, event); result.handled || result.err != nil {
		return result
	}

	if app.handleAttachmentKey(event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	if app.handleAutocompletePriorityKey(event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	if result := app.handleInlineListsAndExtensionKey(ctx, event); result.handled || result.err != nil {
		return result
	}

	if app.handlePreEditorKey(ctx, event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
}

func (app *App) handleInspectionAndModalPriorityKey(
	ctx context.Context,
	event *tcell.EventKey,
) keyHandlingResult {
	if result := app.handleInterruptPriorityKey(ctx, event); result.handled {
		return result
	}

	if result := app.handleModalPriorityKey(ctx, event); result.handled || result.err != nil {
		return result
	}

	if app.handleInspectionAutocompleteEscape(event) || app.handleAgentTaskSessionEscape(ctx, event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	if app.inspectingWhilePromptRuns() {
		return app.handleReadOnlyInspectionPriorityKey(ctx, event)
	}

	return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
}

func (app *App) handleModalPriorityKey(ctx context.Context, event *tcell.EventKey) keyHandlingResult {
	if app.mode != modePanel || app.panel == nil {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
	}

	if isEscapeKey(event) || !app.inspectingWhilePromptRuns() {
		return app.handlePanelPriorityKey(ctx, event)
	}

	return app.readOnlyInspectionResult()
}

func (app *App) handleInterruptPriorityKey(ctx context.Context, event *tcell.EventKey) keyHandlingResult {
	if app.handleWorkingInterruptKey(ctx, event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	if handled, shouldQuit := app.handleForceExitKey(event); handled {
		return keyHandlingResult{err: nil, shouldQuit: shouldQuit, handled: true}
	}

	return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
}

func (app *App) handleReadOnlyInspectionPriorityKey(
	ctx context.Context,
	event *tcell.EventKey,
) keyHandlingResult {
	if !app.inspectingWhilePromptRuns() {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
	}

	if handled, err := app.handleAgentTaskSummaryPriorityKey(ctx, event); handled || err != nil {
		return keyHandlingResult{err: err, shouldQuit: false, handled: true}
	}

	if app.agentTaskSummaryFocused() || app.handleTranscriptScroll(event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	return app.readOnlyInspectionResult()
}

func (app *App) readOnlyInspectionResult() keyHandlingResult {
	app.setStatus(readOnlyAgentInspectionStatus)

	return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
}

func (app *App) handleInlineListsAndExtensionKey(
	ctx context.Context,
	event *tcell.EventKey,
) keyHandlingResult {
	if handled, err := app.handleAgentTaskSummaryPriorityKey(ctx, event); handled || err != nil {
		return keyHandlingResult{err: err, shouldQuit: false, handled: true}
	}

	if app.agentTaskSummaryFocused() {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	if app.handleTranscriptListPriorityKey(event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	if handled, err := app.handleExtensionKey(ctx, event); handled || err != nil {
		return keyHandlingResult{err: err, shouldQuit: false, handled: true}
	}

	return keyHandlingResult{err: nil, shouldQuit: false, handled: app.transcriptListFocused()}
}

func (app *App) handleForceExitKey(event *tcell.EventKey) (handled, shouldQuit bool) {
	if !app.keys.matches(event, actionForceExit) || !app.composerDraftEmpty() {
		return false, false
	}

	return true, app.handleForceExit()
}

func (app *App) handleAgentTaskSessionEscape(ctx context.Context, event *tcell.EventKey) bool {
	if len(app.agentTaskSessionStack) == 0 || !isEscapeKey(event) {
		return false
	}

	if event.Modifiers()&tcell.ModAlt != 0 {
		app.lastEscape = time.Time{}
		if err := app.leaveAgentTaskSession(ctx); err != nil {
			app.setStatus(err.Error())
		}

		return true
	}

	if time.Since(app.lastEscape) > doubleEscapeDelay {
		app.lastEscape = time.Now()
		if app.inspectingWhilePromptRuns() {
			app.setStatus("escape again to interrupt; Alt+Escape returns to parent session")
		} else {
			app.setStatus("escape again to return to parent session")
		}

		return true
	}

	app.lastEscape = time.Time{}
	if app.inspectingWhilePromptRuns() {
		ownerSessionID := app.activePrompt.SessionID
		app.withSessionView(ownerSessionID, func() {
			app.cancelActivePrompt(ctx)
		})

		return true
	}

	if err := app.leaveAgentTaskSession(ctx); err != nil {
		app.setStatus(err.Error())
	}

	return true
}

func (app *App) handlePanelPriorityKey(ctx context.Context, event *tcell.EventKey) keyHandlingResult {
	if app.mode != modePanel || app.panel == nil {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
	}

	return keyHandlingResult{err: app.handlePanelKey(ctx, event), shouldQuit: false, handled: true}
}

func (app *App) handleInspectionAutocompleteEscape(event *tcell.EventKey) bool {
	if !app.inspectingWhilePromptRuns() ||
		!app.autocompleteActive() ||
		!isEscapeKey(event) ||
		event.Modifiers()&tcell.ModAlt != 0 {
		return false
	}

	app.closeAutocomplete()

	return true
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

func (app *App) insertPastedKey(event *tcell.EventKey) {
	if event.Key() == tcell.KeyRune {
		app.composerBuffer.InsertRune(tui.EventRune(event))
	}

	if event.Key() == tcell.KeyEnter {
		app.composerBuffer.InsertRune('\n')
	}

	if event.Key() == tcell.KeyTab {
		app.composerBuffer.InsertRune('\t')
	}
}

func (app *App) handleInputKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if app.keys.matches(event, actionInputClear) && !app.composerDraftEmpty() {
		app.composerBuffer.Clear()
		app.composerImages = nil
		app.resetPromptHistoryNavigation()
		app.resetAutocompleteSelection()
		app.escapePresses = 0

		return false, nil
	}

	if app.keys.matches(event, actionInputSubmit) {
		return app.submit(ctx, event)
	}

	if app.keys.matches(event, actionInputNewLine) && app.working &&
		event.Modifiers()&tcell.ModShift != 0 {
		return app.submit(ctx, event)
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
		if app.selectedPanelKind == panelWorkflows && app.workflowPanelRunID != "" {
			app.openWorkflowsPanel(ctx)

			return nil
		}

		app.closePanel()

		return nil
	}

	handled, err := app.handleSpecialPanelKey(ctx, event)
	if handled || err != nil {
		return err
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

func (app *App) handleSpecialPanelKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	switch app.selectedPanelKind {
	case panelSessions:
		return app.handleSessionPanelKey(ctx, event), nil
	case panelScopedModels:
		return app.handleScopedModelKey(event), nil
	case panelAgentTasks:
		return app.handleAgentTasksPanelKey(ctx, event)
	case panelWorkflows:
		return app.handleWorkflowsPanelKey(ctx, event)
	case panelModel, panelAuthLogin, panelAuthLogout, panelSettings,
		panelHotkeys, panelChangelog, panelTree:
		return false, nil
	default:
		return false, nil
	}
}
