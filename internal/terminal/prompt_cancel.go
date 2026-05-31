package terminal

import "context"

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
	app.activePrompt = nil
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
