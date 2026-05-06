package terminal

import (
	"context"
	"strings"
	"time"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func (app *App) handleEvent(ctx context.Context, event tcell.Event) (bool, error) {
	switch typedEvent := event.(type) {
	case *tcell.EventResize:
		app.screen.Sync()
		return false, nil
	case *tcell.EventKey:
		return app.handleKey(ctx, typedEvent)
	default:
		return false, nil
	}
}

func (app *App) handleKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if app.mode == modePanel && app.panel != nil {
		return app.handlePanelKey(ctx, event)
	}
	if app.handleGlobalShortcut(ctx, event) {
		return false, nil
	}
	if app.keys.matches(event, actionExit) && app.editor.empty() {
		return true, nil
	}
	if app.keys.matches(event, actionInputSubmit) {
		return app.submit(ctx)
	}
	if app.keys.matches(event, actionInputNewLine) {
		app.editor.insertRune('\n')
		return false, nil
	}
	app.handleEditorKey(event)

	return false, nil
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
		{action: actionClear, handler: app.handleClear},
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
	actions := app.editorActions()
	for _, action := range actions {
		if app.keys.matches(event, action.action) {
			action.handler()
			return
		}
	}
	if event.Key() == tcell.KeyRune {
		app.editor.insertRune(eventRune(event))
	}
}

func (app *App) editorActions() []shortcutHandler {
	return []shortcutHandler{
		{action: actionCursorLeft, handler: app.editor.moveLeft},
		{action: actionCursorRight, handler: app.editor.moveRight},
		{action: actionCursorWordLeft, handler: app.editor.moveWordLeft},
		{action: actionCursorWordRight, handler: app.editor.moveWordRight},
		{action: actionCursorLineStart, handler: app.editor.moveLineStart},
		{action: actionCursorLineEnd, handler: app.editor.moveLineEnd},
		{action: actionDeleteCharBackward, handler: app.editor.backspace},
		{action: actionDeleteCharForward, handler: app.editor.deleteForward},
		{action: actionDeleteWordBackward, handler: app.editor.deleteWordBackward},
		{action: actionDeleteWordForward, handler: app.editor.deleteWordForward},
		{action: actionDeleteToLineStart, handler: app.editor.deleteToLineStart},
		{action: actionDeleteToLineEnd, handler: app.editor.deleteToLineEnd},
	}
}

func (app *App) handlePanelKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	action := app.panel.handleKey(event, app.keys)
	switch action.Type {
	case panelActionCancel:
		app.closePanel()
	case panelActionSelect:
		return false, app.applyPanelSelection(ctx, action.Value)
	case panelActionNone:
		return false, nil
	}

	return false, nil
}

func (app *App) handleClear() {
	if app.editor.empty() {
		app.setStatus("press Ctrl+D to exit")
		return
	}
	app.editor.clear()
	app.setStatus("editor cleared")
}

func (app *App) handleEscape(ctx context.Context) {
	if !app.editor.empty() {
		app.editor.clear()
		app.setStatus("editor cleared")
		return
	}
	if time.Since(app.lastEscape) <= doubleEscapeDelay {
		app.openTreePanel(ctx)
		app.lastEscape = time.Time{}
		return
	}
	app.lastEscape = time.Now()
	app.setStatus("escape again to open /tree")
}

func (app *App) submit(ctx context.Context) (bool, error) {
	text := strings.TrimSpace(app.editor.clear())
	if text == "" {
		return false, nil
	}
	if strings.HasPrefix(text, "/") {
		return app.submitCommand(ctx, text)
	}

	return false, app.sendPrompt(ctx, text)
}

func (app *App) sendPrompt(ctx context.Context, text string) error {
	app.addMessage(database.RoleUser, text)
	app.working = true
	app.draw()
	response, err := app.runtime.Prompt(ctx, assistant.PromptRequest{
		ParentEntryID: app.pendingParentID,
		SessionID:     app.sessionID,
		CWD:           app.cwd,
		Text:          text,
		Name:          "",
	})
	app.working = false
	app.pendingParentID = nil
	if err != nil {
		return err
	}
	app.sessionID = response.SessionID
	app.addMessage(database.RoleAssistant, response.Text)

	return nil
}

func (app *App) toggleToolsExpanded() {
	app.toolsExpanded = !app.toolsExpanded
	app.setStatus("tool output expanded: " + boolText(app.toolsExpanded))
}

func (app *App) toggleThinkingHidden() {
	app.hideThinking = !app.hideThinking
	app.setStatus("thinking hidden: " + boolText(app.hideThinking))
}

func (app *App) queueFollowUp() {
	text := strings.TrimSpace(app.editor.clear())
	if text == "" {
		app.setStatus("no follow-up text to queue")
		return
	}
	app.queuedMessages = append(app.queuedMessages, text)
	app.setStatus("queued follow-up")
}

func (app *App) dequeueFollowUp() {
	if len(app.queuedMessages) == 0 {
		app.setStatus("no queued messages")
		return
	}
	lastIndex := len(app.queuedMessages) - 1
	app.editor.setText(app.queuedMessages[lastIndex])
	app.queuedMessages = app.queuedMessages[:lastIndex]
	app.setStatus("restored queued message")
}

func (app *App) cycleThinking() {
	levels := []model.ThinkingLevel{
		model.ThinkingOff,
		model.ThinkingMinimal,
		model.ThinkingLow,
		model.ThinkingMedium,
		model.ThinkingHigh,
		model.ThinkingXHigh,
	}
	current := app.currentThinkingLevel()
	for index, level := range levels {
		if string(level) == current {
			app.setThinkingLevel(string(levels[(index+1)%len(levels)]))
			return
		}
	}
	app.setThinkingLevel(string(model.ThinkingOff))
}

func boolText(value bool) string {
	if value {
		return "on"
	}

	return "off"
}
