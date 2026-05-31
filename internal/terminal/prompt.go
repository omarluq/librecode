package terminal

import (
	"context"
	"strings"
	"time"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

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
	app.tokenUsage = model.EmptyTokenUsage()
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
	app.workStartedAt = time.Now()
	app.workFrame = 0
	go func() {
		defer cancel()
		response, err := app.runtime.Prompt(promptCtx, request)
		if err != nil {
			app.postAsyncEvent(ctx, &asyncEvent{
				Response:  nil,
				ToolEvent: nil,
				Usage:     nil,
				Kind:      asyncEventPromptError,
				Provider:  "",
				Text:      err.Error(),
				PromptID:  promptID,
			})
			return
		}
		app.postAsyncEvent(ctx, &asyncEvent{
			Response:  response,
			ToolEvent: nil,
			Usage:     nil,
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
	app.applyTokenUsage(&response.Usage)
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
