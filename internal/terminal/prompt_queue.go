package terminal

import (
	"context"
	"strings"
)

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
