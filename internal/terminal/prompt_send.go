package terminal

import (
	"context"
	"time"

	"github.com/omarluq/librecode/internal/transcript"

	"github.com/omarluq/librecode/internal/assistant"
)

func (app *App) sendPrompt(ctx context.Context, text string) {
	app.sendPromptWithVisibility(ctx, text, true)
}

func (app *App) sendPromptHidden(ctx context.Context, text string) {
	app.sendPromptWithVisibility(ctx, text, false)
}

func (app *App) sendPromptWithVisibility(ctx context.Context, text string, visible bool) {
	if app.busy() {
		app.queuePrompt(text, visible)

		return
	}

	promptCtx, cancel := context.WithCancel(ctx)
	parentEntryID := cloneStringPtr(app.pendingParentID)
	promptID := app.nextPromptID()
	request := &assistant.PromptRequest{
		OnEvent:        app.promptStreamHandler(promptCtx, promptID),
		OnRetry:        app.promptRetryHandler(promptCtx, promptID),
		OnUserEntry:    app.promptUserEntryHandler(ctx, promptID),
		ParentEntryID:  parentEntryID,
		SessionID:      app.sessionID,
		CWD:            app.cwd,
		Text:           text,
		Name:           "",
		ResumeLatest:   false,
		HideUserPrompt: !visible,
	}
	app.pendingParentID = nil
	app.scrollOffset = 0
	app.streamingText = ""
	app.streamingThinkingText = ""
	app.resetStreamingBlocks()
	app.streamedToolEvents = 0

	app.activePrompt = &activePromptState{
		Cancel:        cancel,
		ParentEntryID: cloneStringPtr(parentEntryID),
		ID:            promptID,
		SessionID:     app.sessionID,
		UserEntryID:   "",
		Prompt:        text,
		Canceled:      false,
	}
	if visible {
		app.addMessage(transcript.RoleUser, text)
	}

	app.working = true
	app.workStartedAt = time.Now()

	app.workFrame = 0
	go app.runPrompt(ctx, promptCtx, cancel, request, promptID)
}

func (app *App) runPrompt(
	ctx context.Context,
	promptCtx context.Context,
	cancel context.CancelFunc,
	request *assistant.PromptRequest,
	promptID uint64,
) {
	defer cancel()

	response, err := app.runtime.Prompt(promptCtx, request)
	if err != nil {
		app.postPromptError(ctx, promptID, err)

		return
	}

	app.postPromptDone(ctx, promptID, response)
}

func (app *App) postPromptError(ctx context.Context, promptID uint64, err error) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventPromptError,
		Provider:      "",
		Text:          err.Error(),
		PromptID:      promptID,
	})
}

func (app *App) postPromptDone(ctx context.Context, promptID uint64, response *assistant.PromptResponse) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response:      response,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventPromptDone,
		Provider:      "",
		Text:          "",
		PromptID:      promptID,
	})
}
