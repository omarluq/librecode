package terminal

import (
	"context"
	"strings"
)

func (app *App) submit(ctx context.Context) (bool, error) {
	draft := app.currentDraft()
	if draft.empty() {
		return false, nil
	}

	if !app.draftCanSubmit(draft) {
		return false, nil
	}

	consumed, err := app.applyPromptSubmitExtensions(ctx)
	if consumed || err != nil {
		return false, err
	}

	draft = app.currentDraft()
	if !app.draftCanSubmit(draft) {
		return false, nil
	}

	draft = app.consumeDraft()
	if draft.empty() {
		return false, nil
	}

	if app.compacting {
		return app.submitDuringCompaction(draft)
	}

	app.recordPromptDraftHistory(draft)

	if strings.HasPrefix(draft.Text, "/") {
		return app.submitCommand(ctx, draft.Text)
	}

	if app.working {
		app.queueDraft(draft, true)

		return false, nil
	}

	app.sendDraft(ctx, draft, true)

	return false, nil
}

func (app *App) draftCanSubmit(draft promptDraft) bool {
	if !app.validateDraftModel(draft) {
		return false
	}

	if strings.HasPrefix(draft.Text, "/") && len(draft.Images) > 0 {
		app.setStatus("slash commands do not accept image attachments")

		return false
	}

	return true
}

func (app *App) submitDuringCompaction(draft promptDraft) (bool, error) {
	if strings.HasPrefix(draft.Text, "/") {
		app.restoreDraft(draft)
		app.setStatus("wait for context compaction to finish")

		return false, nil
	}

	app.recordPromptDraftHistory(draft)
	app.queueDraft(draft, true)
	app.setStatus("queued prompt until context compaction finishes")

	return false, nil
}
