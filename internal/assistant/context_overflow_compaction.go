// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"sync/atomic"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
)

const providerContextOverflowCompactionFailed = "provider context overflow compaction failed"

type providerOverflowRecoveryInput struct {
	onRetry         RetryEventHandler
	preparation     *completionRequestPreparationInput
	build           *contextRequestBuild
	compactionEntry *database.EntryEntity
	recovery        *providerOverflowRecoveryState
}

type providerOverflowRecoveryState struct {
	authoritativeProviderAttempt atomic.Int64
	recoveryAttemptAuthorized    atomic.Bool
	consumed                     atomic.Bool
}

func newProviderOverflowRecoveryState() *providerOverflowRecoveryState {
	return &providerOverflowRecoveryState{
		authoritativeProviderAttempt: atomic.Int64{},
		recoveryAttemptAuthorized:    atomic.Bool{},
		consumed:                     atomic.Bool{},
	}
}

func (state *providerOverflowRecoveryState) reserve() bool {
	return state != nil && state.consumed.CompareAndSwap(false, true)
}

func validProviderOverflowRecoveryInput(input *providerOverflowRecoveryInput) bool {
	return input != nil && input.build != nil && input.build.Request != nil && input.preparation != nil &&
		input.recovery != nil && input.preparation.auth != nil && input.preparation.lineage != nil
}

func (runtime *Runtime) completeWithProviderOverflowRecovery(
	ctx context.Context,
	input *providerOverflowRecoveryInput,
) (*contextRequestBuild, *database.EntryEntity, *CompletionResult, error) {
	if !validProviderOverflowRecoveryInput(input) {
		err := errors.New("nil provider overflow recovery input")

		return nil, nil, nil, oops.In("assistant").
			Code("context_overflow_recovery_input").
			Wrapf(err, "context: invalid overflow recovery input")
	}

	result, err := runtime.completeWithRetry(
		ctx, input.build.Request, input.onRetry, input.recovery.authorizeProviderAttempt,
	)
	classification := classifyCompletion(input.build.Request, result, err)

	if !runtime.reserveProviderOverflowRecovery(input, &classification) {
		return unrecoveredProviderResult(input, result, err, &classification)
	}

	overflowErr := err
	if overflowErr == nil {
		overflowErr = overflowMetadataError(&classification)
	}

	recoveredBuild, recoveredEntry, recoveredResult, recoverErr := runtime.recoverProviderContextOverflow(
		ctx,
		input,
		overflowErr,
	)
	if recoverErr != nil {
		return input.build, input.compactionEntry, nil, recoverErr
	}

	return recoveredBuild, recoveredEntry, recoveredResult, nil
}

func unrecoveredProviderResult(
	input *providerOverflowRecoveryInput,
	result *CompletionResult,
	err error,
	classification *ResponseClassification,
) (*contextRequestBuild, *database.EntryEntity, *CompletionResult, error) {
	if err != nil {
		return input.build, input.compactionEntry, nil, err
	}

	if classification.Class == ResponseMetadataContextOverflow {
		return input.build, input.compactionEntry, nil, overflowMetadataError(classification)
	}

	return input.build, input.compactionEntry, result, nil
}

func (runtime *Runtime) recoverProviderContextOverflow(
	ctx context.Context,
	input *providerOverflowRecoveryInput,
	providerErr error,
) (*contextRequestBuild, *database.EntryEntity, *CompletionResult, error) {
	if !runtime.requestIdentityIsActive(input.build.Request, input) {
		return input.build, input.compactionEntry, nil, providerErr
	}

	currentParentID := input.preparation.lineage.activeParentEntryID

	recoveredEntry, err := runtime.compactAfterProviderOverflow(ctx, input, currentParentID, providerErr)
	if err != nil {
		return nil, nil, nil, err
	}

	input.preparation.lineage.adopt(recoveredEntry)

	recoveredBuild, err := runtime.buildCompletionRequest(ctx, &completionRequestBuildInput{
		selectedModel: input.preparation.selectedModel,
		auth:          *input.preparation.auth,
		onEvent:       input.preparation.onEvent,
		sessionID:     input.preparation.sessionID,
		entryID:       input.preparation.lineage.activeParentEntryID,
		cwd:           input.preparation.cwd,
		prompt:        input.preparation.prompt,
		lineage:       input.preparation.lineage,
	})
	if err != nil {
		runtime.emitContextCompactionError(
			ctx,
			input.preparation.onEvent,
			providerContextOverflowCompactionFailed,
			err,
		)

		return nil, recoveredEntry, nil, oops.In("assistant").
			Code("context_overflow_rebuild").
			Wrapf(err, "context: rebuild completion request after provider overflow compaction")
	}

	runtime.emitUsageSnapshot(ctx, input.preparation.onEvent, &recoveredBuild.Context.Usage)

	if validationErr := runtime.validateOverflowRecoveryBuild(ctx, input, recoveredBuild); validationErr != nil {
		return nil, recoveredEntry, nil, validationErr
	}

	runtime.emitContextCompactionEvent(
		ctx,
		input.preparation.onEvent,
		StreamEventContextCompactionDone,
		compactionMessage("context auto-compacted after provider overflow", input.build.Budget, recoveredEntry),
	)

	prepareRecoveredProviderRequest(recoveredBuild.Request, input.recovery)

	if !runtime.requestIdentityIsActive(recoveredBuild.Request, input) {
		return recoveredBuild, recoveredEntry, nil, providerErr
	}

	result, err := runtime.completeWithRetry(
		ctx, recoveredBuild.Request, input.onRetry, input.recovery.authorizeProviderAttempt,
	)

	classification := classifyCompletion(recoveredBuild.Request, result, err)
	if isContextOverflowClassification(classification.Class) {
		if err != nil {
			return recoveredBuild, recoveredEntry, nil, err
		}

		return recoveredBuild, recoveredEntry, nil, overflowMetadataError(&classification)
	}

	if err != nil {
		return recoveredBuild, recoveredEntry, nil, err
	}

	return recoveredBuild, recoveredEntry, result, nil
}

func (runtime *Runtime) validateOverflowRecoveryBuild(
	ctx context.Context,
	input *providerOverflowRecoveryInput,
	build *contextRequestBuild,
) error {
	validationErr := build.Budget.Validate()
	if validationErr == nil {
		return nil
	}

	runtime.emitContextCompactionError(
		ctx,
		input.preparation.onEvent,
		providerContextOverflowCompactionFailed,
		validationErr,
	)

	return oops.In("assistant").
		Code("context_budget_after_provider_overflow_compact").
		Wrapf(validationErr, "context: validate budget after provider overflow compaction")
}

func (state *providerOverflowRecoveryState) authorizeProviderAttempt(attempt int) {
	state.authoritativeProviderAttempt.Store(int64(attempt))
}

func prepareRecoveredProviderRequest(request *CompletionRequest, recovery *providerOverflowRecoveryState) {
	recovery.authoritativeProviderAttempt.Store(1)
	recovery.recoveryAttemptAuthorized.Store(true)

	request.ProviderAttempt = 1
	request.Identity.ProviderAttempt = 1
	request.Identity.RecoveryAttempt = 1
}

func classifyCompletion(request *CompletionRequest, result *CompletionResult, err error) ResponseClassification {
	input := ResponseClassificationInput{
		Err: err, Termination: llm.NewTerminationMetadata("", "", ""), Provider: "", API: "", Model: "",
		FinishReason: llm.FinishReasonUnknown, RequestedMaxOutput: 0, ReportedOutputTokens: 0,
	}
	if request != nil {
		input.Provider = request.Model.Provider
		input.API = request.Model.API
		input.Model = request.Model.ID
		input.RequestedMaxOutput = request.MaxTokens
	}

	if result != nil {
		input.FinishReason = result.FinishReason
		input.Termination = result.Termination
		input.ReportedOutputTokens = result.Usage.OutputTokens
	}

	return ClassifyResponse(input)
}

func (runtime *Runtime) reserveProviderOverflowRecovery(
	input *providerOverflowRecoveryInput,
	classification *ResponseClassification,
) bool {
	return runtime.decideProviderOverflowRecovery(input, classification).Recover && input.recovery.reserve()
}

func (runtime *Runtime) decideProviderOverflowRecovery(
	input *providerOverflowRecoveryInput,
	classification *ResponseClassification,
) OverflowRecoveryDecision {
	request := input.build.Request

	active, ok := runtime.activeRequestIdentity(request, input)
	if !ok {
		active = emptyRequestIdentity()
	}

	return DecideOverflowRecovery(OverflowRecoveryDecisionInput{
		Classification: *classification,
		Identity:       request.Identity,
		ActiveIdentity: active,
		Replay: ReplayState{
			RecoveryConsumed:    input.recovery.consumed.Load(),
			ToolDispatchStarted: request.ToolSideEffectsStarted,
			LineageAdvanced: request.Identity.LineageParentEntryID != input.preparation.lineage.activeParentEntryID ||
				request.Identity.CompactionGeneration != input.preparation.lineage.generation,
		},
		HasCompactionCandidate: true,
	})
}

func (runtime *Runtime) requestIdentityIsActive(
	request *CompletionRequest,
	input *providerOverflowRecoveryInput,
) bool {
	active, ok := runtime.activeRequestIdentity(request, input)

	return ok && request.Identity == active
}

func (runtime *Runtime) activeRequestIdentity(
	request *CompletionRequest,
	input *providerOverflowRecoveryInput,
) (RequestIdentity, bool) {
	if request == nil || input == nil || input.recovery == nil ||
		input.preparation == nil || input.preparation.lineage == nil {
		return emptyRequestIdentity(), false
	}

	selectedModel, err := runtime.selectedModel()
	if err != nil {
		return emptyRequestIdentity(), false
	}

	return RequestIdentity{
		LogicalRequestID:     input.preparation.lineage.runID,
		Provider:             selectedModel.Provider,
		Model:                selectedModel.ID,
		LineageParentEntryID: input.preparation.lineage.activeParentEntryID,
		ProviderAttempt:      int(input.recovery.authoritativeProviderAttempt.Load()),
		CompactionGeneration: input.preparation.lineage.generation,
		RecoveryAttempt:      activeRecoveryAttempt(input.recovery),
	}, true
}

func activeRecoveryAttempt(recovery *providerOverflowRecoveryState) uint8 {
	if recovery.recoveryAttemptAuthorized.Load() {
		return 1
	}

	return 0
}

func isContextOverflowClassification(class ResponseClass) bool {
	return class == ResponseExplicitContextOverflow || class == ResponseMetadataContextOverflow
}

func overflowMetadataError(classification *ResponseClassification) error {
	return oops.In("assistant").Code("context_window_exceeded").
		With("overflow_signal", classification.ContextOverflowSignal).
		Errorf("provider reported context overflow in completion metadata")
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

	operation := compaction.Operation{
		ID: pendingCompactionOperationID, Reason: compaction.ReasonProviderOverflow,
		RetryIntent: compaction.RetryAfterCompaction,
	}

	entry, err := runtime.compactSessionFrom(
		ctx, input.preparation.sessionID, input.preparation.cwd, &parentID, operation,
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
			providerContextOverflowCompactionFailed,
			err,
		)

		return nil, oops.In("assistant").
			Code("context_overflow_compact").
			Wrapf(err, "compact context after provider overflow")
	}

	return entry, nil
}
