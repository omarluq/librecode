package terminal

import (
	"context"
	"strings"
)

func (app *App) submit(ctx context.Context) (bool, error) {
	text := strings.TrimSpace(app.composerBuffer.TextValue())
	if text == "" {
		return false, nil
	}
	consumed, err := app.applyPromptSubmitExtensions(ctx)
	if consumed || err != nil {
		return false, err
	}
	text = strings.TrimSpace(app.composerBuffer.Clear())
	if text == "" {
		return false, nil
	}
	if app.compacting {
		if strings.HasPrefix(text, "/") {
			app.composerBuffer.SetText(text)
			app.setStatus("wait for context compaction to finish")
			return false, nil
		}
		app.recordPromptHistory(text)
		app.queueFollowUpText(text)
		app.setStatus("queued prompt until context compaction finishes")
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
