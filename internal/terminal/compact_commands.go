package terminal

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/omarluq/librecode/internal/database"
)

const compactedStatusMessage = "context compacted"

func (app *App) compactSession(ctx context.Context) error {
	if app.runtime == nil {
		return fmt.Errorf("runtime is not configured")
	}
	if app.sessionID == "" {
		return fmt.Errorf("no active session")
	}
	if app.busy() {
		return fmt.Errorf("another operation is already running")
	}

	compactCtx, cancel := context.WithCancel(ctx)
	compactID := app.nextPromptID()
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: cancel, ID: compactID}
	app.workStartedAt = time.Now()
	app.workFrame = 0
	app.scrollOffset = 0
	app.setStatus("compacting context")
	go app.runCompactSession(ctx, compactCtx, cancel, compactID)

	return nil
}

func (app *App) runCompactSession(
	ctx context.Context,
	compactCtx context.Context,
	cancel context.CancelFunc,
	compactID uint64,
) {
	defer cancel()
	entry, err := app.runtime.CompactSession(compactCtx, app.sessionID, app.cwd)
	if err != nil {
		app.postCompactError(ctx, compactID, err)
		return
	}
	app.postCompactDone(ctx, compactID, entry)
}

func (app *App) postCompactDone(ctx context.Context, compactID uint64, entry *database.EntryEntity) {
	app.postAsyncEvent(ctx, &asyncEvent{
		Response:  nil,
		ToolEvent: nil,
		Usage:     nil,
		Kind:      asyncEventCompactDone,
		Provider:  firstKeptEntryID(entry),
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

func (app *App) handleCompactAsyncEvent(payload *asyncEvent) bool {
	switch payload.Kind {
	case asyncEventCompactDone:
		app.applyCompactDone(payload)
		return true
	case asyncEventCompactError:
		app.applyCompactError(payload)
		return true
	case asyncEventAuthURL,
		asyncEventAuthDone,
		asyncEventAuthError,
		asyncEventPromptDone,
		asyncEventPromptUserEntry,
		asyncEventPromptDelta,
		asyncEventPromptThinkingDelta,
		asyncEventPromptToolStart,
		asyncEventPromptToolResult,
		asyncEventPromptRetry,
		asyncEventPromptUsage,
		asyncEventPromptError:
		return false
	}

	return false
}

func (app *App) applyCompactDone(payload *asyncEvent) {
	if app.ignoreCompactEvent(payload) {
		return
	}
	app.pendingParentID = nonEmptyStringPtr(payload.Provider)
	app.compacting = false
	app.activeCompaction = nil
	app.addSystemMessage(payload.Text)
	app.setStatus(compactedStatusMessage)
}

func (app *App) applyCompactError(payload *asyncEvent) {
	if app.ignoreCompactEvent(payload) {
		return
	}
	app.compacting = false
	app.activeCompaction = nil
	if payload.Text == "" {
		app.addSystemMessage("context compaction failed")
		return
	}
	app.addSystemMessage(payload.Text)
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

func firstKeptEntryID(entry *database.EntryEntity) string {
	if entry == nil {
		return ""
	}

	return entry.CompactionFirstKeptEntryID
}

func nonEmptyStringPtr(value string) *string {
	if strings.TrimSpace(value) == "" {
		return nil
	}

	return &value
}
