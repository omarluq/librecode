// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

const (
	compactionSourceCore      = "core"
	compactionSourceExtension = "extension"
	compactionTokensBeforeKey = "tokens_before"
	compactionPhaseKey        = "phase"
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
	plan *compactionPlan,
) (*compactionLifecycleDecision, error) {
	payload := compactionPreparationPayload(sessionID, cwd, plan)
	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleSessionBeforeCompact, payload)
	runtime.emitLifecycleDiagnostics(
		ctx,
		extension.LifecycleSessionBeforeCompact,
		&result,
		compactionLifecycleDiagnostics(plan, "before"),
	)
	if err != nil {
		return nil, oops.In("assistant").Code("compact_before_hook").Wrapf(err, "dispatch before compact lifecycle")
	}
	if result.Compaction.Cancel || result.Stopped {
		return nil, compactNothingToDoError("context compaction was canceled by an extension")
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
	plan *compactionPlan,
	fromHook bool,
) {
	payload := compactionSavedPayload(sessionID, cwd, entry, plan, fromHook)
	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleSessionCompact, payload)
	runtime.emitLifecycleDiagnostics(
		ctx,
		extension.LifecycleSessionCompact,
		&result,
		compactionLifecycleDiagnostics(plan, "after"),
	)
	if err != nil {
		return
	}
}

func compactionDecisionFromMutation(
	mutation extension.CompactionMutation,
	plan *compactionPlan,
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
				Errorf("extension compaction summary was empty")
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

func compactionPreparationPayload(sessionID, cwd string, plan *compactionPlan) map[string]any {
	return map[string]any{
		lifecycleCWDKey:             cwd,
		jsonSessionIDKey:            sessionID,
		"first_kept_entry_id":       plan.FirstKeptEntryID,
		compactionTokensBeforeKey:   plan.TokensBefore,
		"summary_message_count":     len(plan.Messages),
		"summarized_entry_ids":      stringSlicePayload(plan.SummarizedEntryIDs),
		"kept_entry_ids":            stringSlicePayload(plan.KeptEntryIDs),
		"split_turn_summary":        plan.SplitTurnSummary,
		compactionFileOperationsKey: compactionFileOperationsPayload(plan.FileOperations),
	}
}

func compactionSavedPayload(
	sessionID string,
	cwd string,
	entry *database.EntryEntity,
	plan *compactionPlan,
	fromHook bool,
) map[string]any {
	payload := compactionPreparationPayload(sessionID, cwd, plan)
	payload[lifecycleEntryIDKey] = ""
	payload[jsonSummaryKey] = ""
	payload["source"] = compactionSourceCore
	if entry != nil {
		payload[lifecycleEntryIDKey] = entry.ID
		payload[jsonSummaryKey] = entry.Summary
	}
	if fromHook {
		payload["source"] = compactionSourceExtension
	}

	return payload
}

func compactionLifecycleDiagnostics(plan *compactionPlan, phase string) map[string]any {
	if plan == nil {
		return map[string]any{compactionPhaseKey: phase}
	}

	return map[string]any{
		compactionPhaseKey:        phase,
		"summarized_entries":      len(plan.SummarizedEntryIDs),
		"kept_entries":            len(plan.KeptEntryIDs),
		"file_operation_count":    len(plan.FileOperations),
		"has_split_turn_summary":  strings.TrimSpace(plan.SplitTurnSummary) != "",
		compactionTokensBeforeKey: plan.TokensBefore,
		"first_kept_entry_id":     plan.FirstKeptEntryID,
	}
}

func compactionFileOperationsPayload(operations []compactionFileOperation) []any {
	payload := make([]any, 0, len(operations))
	for index := range operations {
		operation := operations[index]
		payload = append(payload, map[string]any{
			"entry_id": operation.EntryID,
			"action":   operation.Action,
			"path":     operation.Path,
			"tool":     operation.Tool,
			"command":  operation.Command,
		})
	}

	return payload
}

func stringSlicePayload(values []string) []any {
	payload := make([]any, 0, len(values))
	for _, value := range values {
		payload = append(payload, value)
	}

	return payload
}
