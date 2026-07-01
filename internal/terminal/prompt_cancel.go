package terminal

import "context"

func (app *App) nextPromptID() uint64 {
	app.promptSequence++

	return app.promptSequence
}

func (app *App) cancelActiveOperation(ctx context.Context) {
	if app.compacting {
		app.cancelActiveCompaction()

		return
	}

	app.cancelActivePrompt(ctx)
}

func (app *App) cancelActiveCompaction() {
	if app.activeCompaction != nil {
		app.activeCompaction.Cancel()
	}

	app.activeCompaction = nil
	app.compacting = false
	app.setStatus("context compaction canceled")
}

func (app *App) cancelActivePrompt(_ context.Context) {
	if app.activePrompt == nil {
		app.working = false
		app.streamingText = ""
		app.streamingThinkingText = ""
		app.resetStreamingBlocks()
		app.streamedToolEvents = 0
		app.setStatus("no active response to cancel")

		return
	}

	if app.activePrompt.Canceled {
		app.setStatus("canceling response...")

		return
	}

	app.activePrompt.Canceled = true
	app.activePrompt.Cancel()
	app.setStatus("canceling response...")
}

func cloneStringPtr(value *string) *string {
	if value == nil {
		return nil
	}

	cloned := *value

	return &cloned
}
