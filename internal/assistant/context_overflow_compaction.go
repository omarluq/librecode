// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
)

type providerOverflowRecoveryInput struct {
	preparation     *completionRequestPreparationInput
	build           *contextRequestBuild
	compactionEntry *database.EntryEntity
	onRetry         RetryEventHandler
}

func (runtime *Runtime) completeWithProviderOverflowRecovery(
	ctx context.Context,
	input *providerOverflowRecoveryInput,
) (*contextRequestBuild, *database.EntryEntity, *CompletionResult, error) {
	if input == nil || input.build == nil || input.preparation == nil || input.preparation.auth == nil {
		err := fmt.Errorf("nil provider overflow recovery input")

		return nil, nil, nil, oops.In("assistant").
			Code("context_overflow_recovery_input").
			Wrapf(err, "context: invalid overflow recovery input")
	}

	result, err := runtime.completeWithRetry(ctx, input.build.Request, input.onRetry)
	if err == nil {
		return input.build, input.compactionEntry, result, nil
	}
	if !IsContextWindowError(err) {
		return input.build, input.compactionEntry, nil, err
	}

	recoveredBuild, recoveredEntry, recoveredResult, recoverErr := runtime.recoverProviderContextOverflow(
		ctx,
		input,
		err,
	)
	if recoverErr != nil {
		return input.build, input.compactionEntry, nil, recoverErr
	}

	return recoveredBuild, recoveredEntry, recoveredResult, nil
}

func (runtime *Runtime) recoverProviderContextOverflow(
	ctx context.Context,
	input *providerOverflowRecoveryInput,
	providerErr error,
) (*contextRequestBuild, *database.EntryEntity, *CompletionResult, error) {
	currentParentID := input.preparation.userEntryID
	if input.compactionEntry != nil {
		currentParentID = input.compactionEntry.ID
	}

	recoveredEntry, err := runtime.compactAfterProviderOverflow(ctx, input, currentParentID, providerErr)
	if err != nil {
		return nil, nil, nil, err
	}

	recoveredBuild, err := runtime.buildCompletionRequest(
		ctx,
		input.preparation.sessionID,
		input.preparation.cwd,
		input.preparation.prompt,
		input.preparation.selectedModel,
		*input.preparation.auth,
		input.preparation.onEvent,
	)
	if err != nil {
		runtime.emitContextCompactionError(
			ctx,
			input.preparation.onEvent,
			"provider context overflow compaction failed",
			err,
		)

		return nil, recoveredEntry, nil, oops.In("assistant").
			Code("context_overflow_rebuild").
			Wrapf(err, "context: rebuild completion request after provider overflow compaction")
	}
	runtime.emitUsageSnapshot(ctx, input.preparation.onEvent, recoveredBuild.Context.Usage)
	if runtime.cfg.Context.PreflightEnabled {
		validationErr := recoveredBuild.Budget.Validate()
		if validationErr != nil {
			runtime.emitContextCompactionError(
				ctx,
				input.preparation.onEvent,
				"provider context overflow compaction failed",
				validationErr,
			)

			return nil, recoveredEntry, nil, oops.In("assistant").
				Code("context_budget_after_provider_overflow_compact").
				Wrapf(validationErr, "context: validate budget after provider overflow compaction")
		}
	}
	runtime.emitContextCompactionEvent(
		ctx,
		input.preparation.onEvent,
		StreamEventContextCompactionDone,
		compactionMessage("context auto-compacted after provider overflow", recoveredBuild.Budget, recoveredEntry),
	)

	result, err := runtime.completeWithRetry(ctx, recoveredBuild.Request, input.onRetry)
	if err != nil {
		return recoveredBuild, recoveredEntry, nil, err
	}

	return recoveredBuild, recoveredEntry, result, nil
}

func (runtime *Runtime) compactAfterProviderOverflow(
	ctx context.Context,
	input *providerOverflowRecoveryInput,
	parentID string,
	providerErr error,
) (*database.EntryEntity, error) {
	runtime.emitContextCompactionEvent(
		ctx,
		input.preparation.onEvent,
		StreamEventContextCompactionStart,
		"provider reported context overflow; attempting compaction before retry...",
	)
	entry, err := runtime.CompactSessionFrom(
		ctx,
		input.preparation.sessionID,
		input.preparation.cwd,
		&parentID,
	)
	if isCompactNothingToDoError(err) {
		runtime.emitContextCompactionErrorMessage(
			ctx,
			input.preparation.onEvent,
			"provider context overflow compaction skipped: nothing to compact",
		)

		return nil, providerErr
	}
	if err != nil {
		runtime.emitContextCompactionError(
			ctx,
			input.preparation.onEvent,
			"provider context overflow compaction failed",
			err,
		)

		return nil, oops.In("assistant").
			Code("context_overflow_compact").
			Wrapf(err, "compact context after provider overflow")
	}

	return entry, nil
}
