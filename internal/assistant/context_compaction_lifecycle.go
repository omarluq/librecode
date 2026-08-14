// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

const (
	compactionSourceCore      = "core"
	compactionSourceExtension = "extension"
)

var errNoCompactionDecision = errors.New("no compaction lifecycle decision")

type compactionLifecycleDecision struct {
	Details          map[string]any
	Summary          string
	FirstKeptEntryID string
	FromHook         bool
}

func (runtime *Runtime) dispatchBeforeCompaction(
	ctx context.Context,
	sessionID string,
	cwd string,
	plan *compaction.Plan,
	operation compaction.Operation,
) (*compactionLifecycleDecision, error) {
	payload := lifecyclepayload.CompactionPreparation(sessionID, cwd, plan)
	payload["reason"] = string(operation.Reason)
	payload["retry_intent"] = string(operation.RetryIntent)

	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleSessionBeforeCompact, payload)
	if err != nil {
		return nil, oops.In("assistant").Code("compact_before_hook").Wrapf(err, "dispatch before compact lifecycle")
	}

	if result.Compaction.Cancel || result.Stopped {
		return nil, assistantError(
			compaction.NothingToDoError("context compaction was canceled by an extension"),
			"cancel compaction",
		)
	}

	decision, err := compactionDecisionFromMutation(result.Compaction, plan)
	if errors.Is(err, errNoCompactionDecision) {
		return nil, errNoCompactionDecision
	}

	return decision, err
}

func (runtime *Runtime) dispatchAfterCompaction(
	ctx context.Context,
	sessionID string,
	cwd string,
	entry *database.EntryEntity,
	plan *compaction.Plan,
	operation compaction.Operation,
	fromHook bool,
) {
	source := compactionSourceCore
	if fromHook {
		source = compactionSourceExtension
	}

	payload := lifecyclepayload.CompactionSavedPayload(lifecyclepayload.CompactionSaved{
		Entry:     entry,
		Plan:      plan,
		SessionID: sessionID,
		CWD:       cwd,
		Source:    source,
	})
	payload["reason"] = string(operation.Reason)
	payload["retry_intent"] = string(operation.RetryIntent)

	_, err := runtime.dispatchLifecycle(ctx, extension.LifecycleSessionCompact, payload)
	if err != nil {
		return
	}
}

func compactionDecisionFromMutation(
	mutation extension.CompactionMutation,
	plan *compaction.Plan,
) (*compactionLifecycleDecision, error) {
	if mutation.Summary == nil && mutation.FirstKeptEntryID == nil && len(mutation.Details) == 0 {
		return nil, errNoCompactionDecision
	}

	decision := &compactionLifecycleDecision{
		Details:          cloneAnyMap(mutation.Details),
		Summary:          "",
		FirstKeptEntryID: "",
		FromHook:         true,
	}
	if mutation.Summary != nil {
		decision.Summary = strings.TrimSpace(*mutation.Summary)
		if decision.Summary == "" {
			return nil, oops.In("assistant").
				Code("compact_hook_empty_summary").
				Wrapf(&compaction.SummaryError{
					Kind: compaction.ErrSummaryEmpty, Cause: nil, Provider: "", Model: "", Reason: "",
					Input: 0, Limit: 0, Before: 0, After: 0,
				}, "extension compaction summary was empty")
		}
	}

	if mutation.FirstKeptEntryID != nil {
		decision.FirstKeptEntryID = strings.TrimSpace(*mutation.FirstKeptEntryID)
		if !slices.Contains(plan.KeptEntryIDs, decision.FirstKeptEntryID) {
			return nil, oops.In("assistant").Code("compact_hook_invalid_first_kept").Errorf(
				"extension compaction first kept entry is outside the planned retained tail",
			)
		}
	}

	return decision, nil
}
