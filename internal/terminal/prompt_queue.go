package terminal

import (
	"context"
	"strings"
)

func (app *App) queueFollowUp() {
	draft := app.consumeDraft()
	if draft.empty() {
		app.setStatus("no follow-up text to queue")

		return
	}

	if !app.validateDraftModel(draft) {
		app.restoreDraft(draft)

		return
	}

	if strings.HasPrefix(draft.Text, "/") && len(draft.Images) > 0 {
		app.restoreDraft(draft)
		app.setStatus("slash commands do not accept image attachments")

		return
	}

	app.recordPromptDraftHistory(draft)
	app.queueDraft(draft, true)
	app.setStatus("follow-up queued")
}

func (app *App) queueFollowUpText(text string) { app.queuePrompt(text, true) }

// queuePrompt keeps text-only internal workflow callers source compatible.
func (app *App) queuePrompt(text string, visible bool) {
	app.queueDraft(promptDraft{Text: strings.TrimSpace(text), Images: nil}, visible)
}

func (app *App) queueDraft(draft promptDraft, visible bool) {
	draft.Text = strings.TrimSpace(draft.Text)
	if draft.empty() {
		return
	}

	draft = clonePromptDraft(draft)
	if visible {
		app.queuedMessages = append(app.queuedMessages, draft)

		return
	}

	app.hiddenQueuedMessages = append(app.hiddenQueuedMessages, draft)
}

func (app *App) processQueuedPrompt(ctx context.Context) {
	if app.busy() {
		return
	}

	if len(app.hiddenQueuedMessages) > 0 {
		draft := app.hiddenQueuedMessages[0]
		app.hiddenQueuedMessages = app.hiddenQueuedMessages[1:]
		app.sendDraft(ctx, draft, false)

		return
	}

	if len(app.queuedMessages) == 0 {
		return
	}

	draft := app.queuedMessages[0]
	app.queuedMessages = app.queuedMessages[1:]
	app.sendDraft(ctx, draft, true)
}

func (app *App) queuedCompactionPrompts() []promptDraft {
	if app.activeCompaction == nil || app.activeCompaction.QueuedStart >= len(app.queuedMessages) {
		return nil
	}

	queued := clonePromptDrafts(app.queuedMessages[app.activeCompaction.QueuedStart:])
	app.queuedMessages = app.queuedMessages[:app.activeCompaction.QueuedStart]

	return queued
}

func (app *App) restoreCompactionQueuedPrompts(queued []promptDraft) {
	if len(queued) == 0 {
		return
	}

	app.queuedMessages = append(app.queuedMessages, clonePromptDrafts(queued)...)
	app.dequeueFollowUp()
}

func (app *App) dequeueFollowUp() {
	if len(app.queuedMessages) == 0 {
		app.setStatus("no queued messages")

		return
	}

	last := len(app.queuedMessages) - 1
	app.resetPromptHistoryNavigation()
	app.restoreDraft(app.queuedMessages[last])
	app.queuedMessages = app.queuedMessages[:last]
	app.setStatus("restored queued message")
}
