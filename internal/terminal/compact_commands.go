package terminal

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const compactedStatusMessage = "context compacted"

func (app *App) compactSession(ctx context.Context) error {
	if app.runtime == nil {
		return errors.New("runtime is not configured")
	}

	if app.sessionID == "" {
		return errors.New("no active session")
	}

	if app.busy() {
		return errors.New("another operation is already running")
	}

	compactCtx, cancel := context.WithCancel(ctx)
	compactID := app.nextPromptID()
	parentEntryID := app.compactionParentEntryID()
	app.compacting = true
	app.activeCompaction = &activeCompactionState{
		Cancel:      cancel,
		ID:          compactID,
		QueuedStart: len(app.queuedMessages),
	}
	app.workStartedAt = time.Now()
	app.workFrame = 0
	app.scrollOffset = 0

	app.setStatus("compacting context")
	go app.runCompactSession(ctx, compactCtx, cancel, compactID, parentEntryID)

	return nil
}

func (app *App) runCompactSession(
	ctx context.Context,
	compactCtx context.Context,
	cancel context.CancelFunc,
	compactID uint64,
	parentEntryID *string,
) {
	defer cancel()

	entry, err := app.runtime.CompactSessionFrom(compactCtx, app.sessionID, app.cwd, parentEntryID)
	if err != nil {
		app.postCompactError(ctx, compactID, err)

		return
	}

	usage, err := app.runtime.ContextUsage(compactCtx, app.sessionID, app.cwd)
	if err != nil {
		app.postCompactDone(ctx, compactID, entry, nil)

		return
	}

	app.postCompactDone(ctx, compactID, entry, &usage)
}

func (app *App) postCompactDone(
	ctx context.Context,
	compactID uint64,
	entry *database.EntryEntity,
	usage *model.TokenUsage,
) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response:  nil,
		ToolEvent: nil,
		Usage:     usage,
		Kind:      asyncEventCompactDone,
		Provider:  compactionEntryID(entry),
		Text:      compactDoneText(entry),
		PromptID:  compactID,
	})
}

func (app *App) postCompactError(ctx context.Context, compactID uint64, err error) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response:  nil,
		ToolEvent: nil,
		Usage:     nil,
		Kind:      asyncEventCompactError,
		Provider:  "",
		Text:      err.Error(),
		PromptID:  compactID,
	})
}

func (app *App) handleCompactAsyncEvent(ctx context.Context, payload *asyncEvent) bool {
	switch payload.Kind {
	case asyncEventCompactDone:
		if app.activeCompaction == nil {
			return false
		}

		app.applyCompactDone(ctx, payload)

		return true
	case asyncEventCompactError:
		if app.activeCompaction == nil {
			return false
		}

		app.applyCompactError(payload)

		return true
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventCompactStart,
		asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptRetry,
		asyncEventPromptUsage,
		asyncEventPromptUsageSnapshot,
		asyncEventPromptError,
		asyncEventPromptContext:
		return false
	}

	return false
}

func (app *App) applyCompactDone(ctx context.Context, payload *asyncEvent) {
	if app.ignoreCompactEvent(payload) {
		return
	}

	app.pendingParentID = nonEmptyStringPtr(payload.Provider)
	app.compacting = false
	app.activeCompaction = nil
	app.applyTokenUsageEvent(payload.Usage, true)
	app.addSystemMessage(payload.Text)
	app.setStatus(compactedStatusMessage)
	app.processQueuedPrompt(ctx)
}

func (app *App) applyCompactError(payload *asyncEvent) {
	if app.ignoreCompactEvent(payload) {
		return
	}

	queued := app.queuedCompactionPrompts()
	app.compacting = false

	app.activeCompaction = nil
	if payload.Text == "" {
		app.addSystemMessage("context compaction failed")
	} else {
		app.addSystemMessage(payload.Text)
	}

	app.restoreCompactionQueuedPrompts(queued)
}

func (app *App) ignoreCompactEvent(payload *asyncEvent) bool {
	return app.activeCompaction == nil || app.activeCompaction.ID != payload.PromptID
}

func compactDoneText(entry *database.EntryEntity) string {
	if entry == nil {
		return compactedStatusMessage
	}

	return fmt.Sprintf(
		"context compacted: summarized %s tokens; kept recent context from entry %s",
		compactCount(entry.CompactionTokensBefore),
		entry.CompactionFirstKeptEntryID,
	)
}

func compactionEntryID(entry *database.EntryEntity) string {
	if entry == nil {
		return ""
	}

	return entry.ID
}

func (app *App) compactionParentEntryID() *string {
	if app.pendingParentID != nil {
		return cloneStringPtr(app.pendingParentID)
	}

	return nil
}

func nonEmptyStringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}
