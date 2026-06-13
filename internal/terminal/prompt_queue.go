package terminal

import (
	"context"
	"strings"
)

func (app *App) queueFollowUp() {
	text := strings.TrimSpace(app.composerBuffer.Clear())
	if text == "" {
		app.setStatus("no follow-up text to queue")

		return
	}

	app.recordPromptHistory(text)
	app.queueFollowUpText(text)
}

func (app *App) queueFollowUpText(text string) {
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	app.queuedMessages = append(app.queuedMessages, text)
}

func (app *App) processQueuedPrompt(ctx context.Context) {
	if app.busy() || len(app.queuedMessages) == 0 {
		return
	}

	text := app.queuedMessages[0]
	app.queuedMessages = app.queuedMessages[1:]
	app.sendPrompt(ctx, text)
}

func (app *App) queuedCompactionPrompts() []string {
	if app.activeCompaction == nil || app.activeCompaction.QueuedStart >= len(app.queuedMessages) {
		return nil
	}

	queued := append([]string(nil), app.queuedMessages[app.activeCompaction.QueuedStart:]...)
	app.queuedMessages = app.queuedMessages[:app.activeCompaction.QueuedStart]

	return queued
}

func (app *App) restoreCompactionQueuedPrompts(queued []string) {
	if len(queued) == 0 {
		return
	}

	app.queuedMessages = append(app.queuedMessages, queued...)
	app.dequeueFollowUp()
}

func (app *App) dequeueFollowUp() {
	if len(app.queuedMessages) == 0 {
		app.setStatus("no queued messages")

		return
	}

	lastIndex := len(app.queuedMessages) - 1
	app.resetPromptHistoryNavigation()
	app.composerBuffer.SetText(app.queuedMessages[lastIndex])
	app.queuedMessages = app.queuedMessages[:lastIndex]
	app.setStatus("restored queued message")
}
