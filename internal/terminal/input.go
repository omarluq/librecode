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
	if handled, err := app.handleExtensionKey(ctx, event); handled || err != nil {
		return keyHandlingResult{err: err, shouldQuit: false, handled: true}
	}
	if app.keys.matches(event, actionForceExit) {
		return keyHandlingResult{err: nil, shouldQuit: app.handleForceExit(), handled: true}
	}
	if app.mode == modePanel && app.panel != nil {
		return keyHandlingResult{err: app.handlePanelKey(ctx, event), shouldQuit: false, handled: true}
	}
	if app.handlePreEditorKey(ctx, event) {
		return keyHandlingResult{err: nil, shouldQuit: false, handled: true}
	}

	return keyHandlingResult{err: nil, shouldQuit: false, handled: false}
}

func (app *App) handleInputKey(ctx context.Context, event *tcell.EventKey) (bool, error) {
	if app.keys.matches(event, actionInputSubmit) {
		return app.submit(ctx)
	}
	if app.keys.matches(event, actionInputNewLine) {
		app.resetPromptHistoryNavigation()
		app.insertComposerRune('\n')
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
	actions := app.editorActions()
	for _, action := range actions {
		if app.keys.matches(event, action.action) {
			app.resetPromptHistoryNavigation()
			action.handler()
			return
		}
	}
	if event.Key() == tcell.KeyRune {
		app.resetPromptHistoryNavigation()
		app.insertComposerRune(eventRune(event))
	}
}

func (app *App) editorActions() []shortcutHandler {
	return []shortcutHandler{
		app.composerShortcut(actionCursorLeft, app.moveComposerLeft),
		app.composerShortcut(actionCursorRight, app.moveComposerRight),
		app.composerShortcut(actionCursorWordLeft, app.moveComposerWordLeft),
		app.composerShortcut(actionCursorWordRight, app.moveComposerWordRight),
		app.composerShortcut(actionCursorLineStart, app.moveComposerLineStart),
		app.composerShortcut(actionCursorLineEnd, app.moveComposerLineEnd),
		app.composerShortcut(actionDeleteCharBackward, app.deleteComposerBackward),
		app.composerShortcut(actionDeleteCharForward, app.deleteComposerForward),
		app.composerShortcut(actionDeleteWordBackward, app.deleteComposerWordBackward),
		app.composerShortcut(actionDeleteWordForward, app.deleteComposerWordForward),
		app.composerShortcut(actionDeleteToLineStart, app.deleteComposerToLineStart),
		app.composerShortcut(actionDeleteToLineEnd, app.deleteComposerToLineEnd),
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
	action := app.panel.handleKey(event, app.keys)
	switch action.Type {
	case panelActionCancel:
		app.closePanel()
	case panelActionSelect:
		return app.applyPanelSelection(ctx, action.Value)
	case panelActionNone:
		return nil
	}

	return nil
}

func (app *App) handleForceExit() bool {
	if time.Since(app.lastControlC) <= doubleControlCDelay {
		return true
	}
	app.lastControlC = time.Now()
	app.setStatus("press Ctrl+C again to exit")

	return false
}

func (app *App) handleEscape(ctx context.Context) {
	if app.working {
		if time.Since(app.lastEscape) <= doubleEscapeDelay {
			app.cancelActivePrompt(ctx)
			app.lastEscape = time.Time{}
			return
		}
		app.lastEscape = time.Now()
		app.setStatus("escape again to cancel response")
		return
	}
	if !app.composerEmpty() {
		app.clearComposer()
		app.resetPromptHistoryNavigation()
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
	text := strings.TrimSpace(app.composerText())
	if text == "" {
		return false, nil
	}
	consumed, err := app.applyPromptSubmitExtensions(ctx)
	if consumed || err != nil {
		return false, err
	}
	text = strings.TrimSpace(app.clearComposer())
	if text == "" {
		return false, nil
	}
	app.recordPromptHistory(text)
	if strings.HasPrefix(text, "/") {
		return app.submitCommand(ctx, text)
	}
	if app.working {
		app.queueFollowUpText(text)
		return false, nil
	}

	app.sendPrompt(ctx, text)
	return false, nil
}

func (app *App) sendPrompt(ctx context.Context, text string) {
	if app.working {
		app.queueFollowUpText(text)
		return
	}
	promptCtx, cancel := context.WithCancel(ctx)
	parentEntryID := cloneStringPtr(app.pendingParentID)
	promptID := app.nextPromptID()
	request := &assistant.PromptRequest{
		OnEvent:       app.promptStreamHandler(promptCtx, promptID),
		OnRetry:       app.promptRetryHandler(promptCtx, promptID),
		OnUserEntry:   app.promptUserEntryHandler(promptCtx, promptID),
		ParentEntryID: parentEntryID,
		SessionID:     app.sessionID,
		CWD:           app.cwd,
		Text:          text,
		Name:          "",
		ResumeLatest:  false,
	}
	app.pendingParentID = nil
	app.scrollOffset = 0
	app.streamingText = ""
	app.streamingThinkingText = ""
	app.resetStreamingBlocks()
	app.streamedToolEvents = 0
	app.activePrompt = &activePromptState{
		Cancel:           cancel,
		ParentEntryID:    cloneStringPtr(parentEntryID),
		ID:               promptID,
		SessionID:        app.sessionID,
		UserEntryID:      "",
		Prompt:           text,
		BaselineMessages: len(app.messages),
		Canceled:         false,
	}
	app.addMessage(database.RoleUser, text)
	app.working = true
	app.workFrame = 0
	go func() {
		defer cancel()
		response, err := app.runtime.Prompt(promptCtx, request)
		if err != nil {
			app.postAsyncEvent(ctx, asyncEvent{
				Response:  nil,
				ToolEvent: nil,
				Kind:      asyncEventPromptError,
				Provider:  "",
				Text:      err.Error(),
				PromptID:  promptID,
			})
			return
		}
		app.postAsyncEvent(ctx, asyncEvent{
			Response:  response,
			ToolEvent: nil,
			Kind:      asyncEventPromptDone,
			Provider:  "",
			Text:      "",
			PromptID:  promptID,
		})
	}()
}

func (app *App) applyPromptResponse(ctx context.Context, response *assistant.PromptResponse, promptID uint64) {
	if app.consumeCanceledPrompt(promptID) {
		return
	}
	if app.activePrompt != nil && app.activePrompt.Canceled {
		app.activePrompt = nil
		return
	}
	streamingBlocks := append([]chatMessage(nil), app.streamingBlocks...)
	app.working = false
	app.streamingText = ""
	app.streamingThinkingText = ""
	app.resetStreamingBlocks()
	if response == nil {
		app.activePrompt = nil
		app.processQueuedPrompt(ctx)
		return
	}
	app.sessionID = response.SessionID
	app.applyPromptResponseSideEffects(response, streamingBlocks)
	app.streamedToolEvents = 0
	app.addMessage(database.RoleAssistant, response.Text)
	app.activePrompt = nil
	app.persistSessionSettings()
	app.processQueuedPrompt(ctx)
}

func (app *App) applyPromptResponseSideEffects(response *assistant.PromptResponse, streamingBlocks []chatMessage) {
	streamedThinkingBlocks := 0
	streamedToolBlocks := app.streamedToolEvents
	if len(streamingBlocks) > 0 {
		streamedThinkingBlocks, streamedToolBlocks = app.applyStreamedSideEffectBlocks(streamingBlocks)
	}
	app.applyRemainingSideEffects(response, streamedThinkingBlocks, streamedToolBlocks)
}

func (app *App) applyStreamedSideEffectBlocks(
	streamingBlocks []chatMessage,
) (streamedThinkingBlocks, streamedToolBlocks int) {
	streamedThinkingBlocks = 0
	streamedToolBlocks = 0
	for _, block := range streamingBlocks {
		thinkingBlock, toolBlock := app.applyStreamedSideEffectBlock(block)
		streamedThinkingBlocks += thinkingBlock
		streamedToolBlocks += toolBlock
	}

	return streamedThinkingBlocks, streamedToolBlocks
}

func (app *App) applyStreamedSideEffectBlock(block chatMessage) (thinkingBlocks, toolBlocks int) {
	switch block.Role {
	case database.RoleThinking:
		return app.applyStreamedThinkingBlock(block.Content), 0
	case database.RoleToolResult, database.RoleBashExecution:
		app.addMessage(block.Role, block.Content)
		return 0, 1
	case database.RoleAssistant,
		database.RoleUser,
		database.RoleCustom,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return 0, 0
	}

	return 0, 0
}

func (app *App) applyStreamedThinkingBlock(content string) int {
	if strings.TrimSpace(content) == "" {
		return 0
	}
	app.addMessage(database.RoleThinking, content)

	return 1
}

func (app *App) applyRemainingSideEffects(
	response *assistant.PromptResponse,
	streamedThinkingBlocks int,
	streamedToolBlocks int,
) {
	for index := streamedThinkingBlocks; index < len(response.Thinking); index++ {
		app.addMessage(database.RoleThinking, response.Thinking[index])
	}
	for index := streamedToolBlocks; index < len(response.ToolEvents); index++ {
		app.addMessage(database.RoleToolResult, formatToolEventForUI(&response.ToolEvents[index]))
	}
}

func (app *App) nextPromptID() uint64 {
	app.promptSequence++

	return app.promptSequence
}

func (app *App) cancelActivePrompt(ctx context.Context) {
	if app.activePrompt == nil {
		app.working = false
		app.streamingText = ""
		app.streamingThinkingText = ""
		app.resetStreamingBlocks()
		app.streamedToolEvents = 0
		app.setStatus("no active response to cancel")
		return
	}

	activePrompt := app.activePrompt
	activePrompt.Canceled = true
	app.canceledPrompts[activePrompt.ID] = activePrompt
	activePrompt.Cancel()
	app.revertActivePromptUI(activePrompt)
	if app.deleteCanceledPromptBranch(ctx, activePrompt) {
		app.setStatus("response canceled; conversation reverted")
	}
}

func (app *App) revertActivePromptUI(activePrompt *activePromptState) {
	if activePrompt.BaselineMessages >= 0 && activePrompt.BaselineMessages <= len(app.messages) {
		app.truncateMessages(activePrompt.BaselineMessages)
	}
	app.pendingParentID = cloneStringPtr(activePrompt.ParentEntryID)
	app.queuedMessages = []string{}
	app.streamingText = ""
	app.streamingThinkingText = ""
	app.resetStreamingBlocks()
	app.streamedToolEvents = 0
	app.working = false
	app.scrollOffset = 0
	if app.composerEmpty() {
		app.setComposerText(activePrompt.Prompt)
	}
}

func (app *App) deleteCanceledPromptBranch(ctx context.Context, activePrompt *activePromptState) bool {
	if activePrompt.SessionID == "" || activePrompt.UserEntryID == "" {
		return true
	}
	err := app.runtime.SessionRepository().DeleteEntryBranch(
		ctx,
		activePrompt.SessionID,
		activePrompt.UserEntryID,
	)
	if err != nil {
		app.setStatus("canceled response; failed to revert persisted branch: " + err.Error())
		return false
	}
	delete(app.canceledPrompts, activePrompt.ID)

	return true
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func (app *App) toggleToolsExpanded() {
	app.toolsExpanded = !app.toolsExpanded
	app.persistSessionSettings()
	app.setStatus("tool output expanded: " + boolText(app.toolsExpanded))
}

func (app *App) toggleThinkingHidden() {
	app.hideThinking = !app.hideThinking
	app.persistSessionSettings()
	app.setStatus("thinking hidden: " + boolText(app.hideThinking))
}

func (app *App) queueFollowUp() {
	text := strings.TrimSpace(app.clearComposer())
	if text == "" {
		app.setStatus("no follow-up text to queue")
		return
	}
	app.recordPromptHistory(text)
	app.queueFollowUpText(text)
}

func (app *App) queueFollowUpText(text string) {
	app.queuedMessages = append(app.queuedMessages, text)
}

func (app *App) processQueuedPrompt(ctx context.Context) {
	if app.working || len(app.queuedMessages) == 0 {
		return
	}
	text := app.queuedMessages[0]
	app.queuedMessages = app.queuedMessages[1:]
	app.sendPrompt(ctx, text)
}

func (app *App) dequeueFollowUp() {
	if len(app.queuedMessages) == 0 {
		app.setStatus("no queued messages")
		return
	}
	lastIndex := len(app.queuedMessages) - 1
	app.resetPromptHistoryNavigation()
	app.setComposerText(app.queuedMessages[lastIndex])
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
