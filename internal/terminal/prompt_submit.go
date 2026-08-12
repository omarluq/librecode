package terminal

import (
	"context"
	"errors"
	"strings"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/extension"
)

type promptDelivery string

const (
	promptDeliveryPrompt   promptDelivery = "prompt"
	promptDeliverySteer    promptDelivery = "steer"
	promptDeliveryFollowUp promptDelivery = "follow_up"
)

func (app *App) submit(ctx context.Context, events ...*tcell.EventKey) (bool, error) {
	delivery := app.promptDeliveryForSubmit(events...)

	key := extension.ComposerKeyEvent{
		Key: "enter", Text: "", Ctrl: false, Alt: false, Shift: false,
	}
	if len(events) > 0 && events[0] != nil {
		key = terminalKeyEvent(events[0])
	}

	draft := app.currentDraft()
	if draft.empty() {
		return false, nil
	}

	if !app.draftCanSubmit(draft) {
		return false, nil
	}

	consumed, err := app.applyPromptSubmitExtensions(ctx, key, delivery)
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

	return app.deliverDraft(ctx, draft, delivery)
}

func (app *App) deliverDraft(
	ctx context.Context,
	draft promptDraft,
	delivery promptDelivery,
) (bool, error) {
	if app.compacting {
		return app.submitDuringCompaction(draft)
	}

	if strings.HasPrefix(draft.Text, "/") {
		app.recordPromptDraftHistory(draft)

		return app.submitCommand(ctx, draft.Text)
	}

	switch delivery {
	case promptDeliveryPrompt:
		app.recordPromptDraftHistory(draft)
		app.sendDraft(ctx, draft, true)
	case promptDeliveryFollowUp:
		app.recordPromptDraftHistory(draft)
		app.queueDraft(draft, true)
		app.setStatus("follow-up queued")
	case promptDeliverySteer:
		if err := app.steerDraft(ctx, draft); err != nil {
			return false, err
		}

		app.recordPromptDraftHistory(draft)
	default:
		app.recordPromptDraftHistory(draft)
		app.sendDraft(ctx, draft, true)
	}

	return false, nil
}

func (app *App) promptDeliveryForSubmit(events ...*tcell.EventKey) promptDelivery {
	if app.working && len(events) > 0 && events[0] != nil &&
		app.keys.matches(events[0], actionInputNewLine) {
		return promptDeliveryFollowUp
	}

	if app.working {
		return promptDeliverySteer
	}

	return promptDeliveryPrompt
}

func (app *App) steerDraft(ctx context.Context, draft promptDraft) error {
	active := app.activePrompt
	if app.runtime == nil || active == nil || active.SessionID == "" ||
		active.SessionID != app.sessionID || active.UserEntryID == "" {
		app.queueSteeringFallback(draft)

		return nil
	}

	err := app.runtime.Steer(ctx, &assistant.SteeringRequest{
		SessionID:      active.SessionID,
		RunID:          active.UserEntryID,
		Text:           draft.Text,
		Images:         draft.assistantImages(),
		HideUserPrompt: false,
	})
	if err == nil {
		app.steeringMessages = append(app.steeringMessages, clonePromptDraft(draft))

		app.setStatus("steering accepted for the active response")

		return nil
	}

	if errors.Is(err, assistant.ErrSteeringInactive) ||
		errors.Is(err, assistant.ErrSteeringClosed) ||
		errors.Is(err, assistant.ErrSteeringStaleRun) {
		app.queueSteeringFallback(draft)

		return nil
	}

	app.restoreDraft(draft)

	return terminalError(err, "submit steering")
}

func (app *App) queueSteeringFallback(draft promptDraft) {
	app.queueDraft(draft, true)
	app.setStatus("response finished; prompt queued next")
}

func (app *App) deliverHiddenContinuation(ctx context.Context, text string) {
	draft := promptDraft{Text: strings.TrimSpace(text), Images: nil}
	if draft.empty() {
		return
	}

	if app.steerHiddenContinuation(ctx, draft) {
		return
	}

	if app.busy() || app.runtime == nil {
		app.queueDraft(draft, false)

		return
	}

	app.sendDraft(ctx, draft, false)
}

func (app *App) steerHiddenContinuation(ctx context.Context, draft promptDraft) bool {
	active := app.activePrompt
	if !app.working || app.runtime == nil || active == nil ||
		active.SessionID != app.sessionID || active.UserEntryID == "" {
		return false
	}

	err := app.runtime.Steer(ctx, &assistant.SteeringRequest{
		SessionID: active.SessionID, RunID: active.UserEntryID,
		Text: draft.Text, Images: nil, HideUserPrompt: true,
	})
	if err == nil {
		app.setStatus("background result steered into the active response")

		return true
	}

	if !errors.Is(err, assistant.ErrSteeringInactive) &&
		!errors.Is(err, assistant.ErrSteeringClosed) &&
		!errors.Is(err, assistant.ErrSteeringStaleRun) {
		app.setStatus("background result could not steer; queued next")
	}

	return false
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
