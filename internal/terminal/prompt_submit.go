package terminal

import (
	"context"
	"strings"
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
	if app.compacting {
		app.setComposerText(text)
		app.setStatus("wait for context compaction to finish")
		return false, nil
	}

	app.sendPrompt(ctx, text)
	return false, nil
}
