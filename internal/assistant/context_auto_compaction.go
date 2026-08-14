// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/compaction"
	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/units"
)

const (
	contextAutoCompactionBeforeRequestFailed = "context auto-compaction before request failed"
	postResponseAutoCompactThresholdPercent  = 80
)

type contextRequestBuild struct {
	Context *contextwindow.BuildResult
	Request *CompletionRequest
	Budget  contextwindow.Budget
}

type completionRequestBuildInput struct {
	selectedModel *model.Model
	onEvent       func(StreamEvent)
	lineage       *promptLineage
	sessionID     string
	entryID       string
	cwd           string
	prompt        string
	auth          model.RequestAuth
}

func (runtime *Runtime) buildCompletionRequest(
	ctx context.Context,
	input *completionRequestBuildInput,
) (*contextRequestBuild, error) {
	contextResult, err := runtime.buildModelContext(
		ctx,
		input.sessionID,
		input.entryID,
		input.cwd,
		input.prompt,
		input.selectedModel,
		input.onEvent,
	)
	if err != nil {
		return nil, oops.In("assistant").Code("context_build_model").Wrapf(err, "context: build model context")
	}

	registry, err := runtime.promptToolRegistry(ctx, input.cwd, input.sessionID)
	if err != nil {
		return nil, oops.In("assistant").Code("context_tool_registry").Wrapf(err, "context: create tool registry")
	}

	request := runtime.modelCompletionRequest(&modelCompletionRequestInput{
		selectedModel: input.selectedModel,
		registry:      registry,
		onEvent:       input.onEvent,
		messages:      contextResult.Messages,
		auth:          input.auth,
		usage:         contextResult.Usage,
		sessionID:     input.sessionID,
		systemPrompt:  contextResult.SystemPrompt,
		cwd:           input.cwd,
		lineage:       input.lineage,
	})
	budget := contextwindow.NewBudget(
		&contextResult.Usage,
		input.selectedModel,
		&runtime.cfg.Context,
		func() int { return runtime.estimateToolSchemaTokens(request) },
	)
	contextResult.Usage = budget.UsageWithBudget(&contextResult.Usage)
	request.Usage = contextResult.Usage

	return &contextRequestBuild{Context: contextResult, Request: request, Budget: budget}, nil
}

type completionRequestPreparationInput struct {
	selectedModel *model.Model
	auth          *model.RequestAuth
	lineage       *promptLineage
	onEvent       func(StreamEvent)
	sessionID     string
	cwd           string
	prompt        string
}

func (runtime *Runtime) prepareCompletionRequestWithAutoCompaction(
	ctx context.Context,
	input *completionRequestPreparationInput,
) (*contextRequestBuild, *database.EntryEntity, error) {
	if input == nil || input.auth == nil || input.lineage == nil {
		err := errors.New("nil completion request preparation input")

		return nil, nil, oops.In("assistant").
			Code("context_prepare_input").
			Wrapf(err, "context: invalid completion preparation input")
	}

	auth := *input.auth

	build, err := runtime.buildCompletionRequest(ctx, &completionRequestBuildInput{
		selectedModel: input.selectedModel,
		auth:          auth,
		onEvent:       input.onEvent,
		sessionID:     input.sessionID,
		entryID:       input.lineage.activeParentEntryID,
		cwd:           input.cwd,
		prompt:        input.prompt,
		lineage:       input.lineage,
	})
	if err != nil {
		return nil, nil, oops.In("assistant").
			Code("context_request_build").
			Wrapf(err, "context: build completion request")
	}

	runtime.emitUsage(ctx, input.onEvent, &build.Context.Usage)

	if !runtime.cfg.Context.PreflightEnabled || !runtime.cfg.Context.AutoCompactionEnabled {
		return build, nil, nil
	}

	originalBudget := build.Budget

	validationErr := originalBudget.Validate()
	if validationErr == nil {
		return build, nil, nil
	}

	compactionEntry, err := runtime.compactBeforeRequest(ctx, input, originalBudget, validationErr)
	if err != nil {
		return nil, nil, err
	}

	input.lineage.adopt(compactionEntry)

	build, err = runtime.rebuildAfterPreRequestCompaction(
		ctx,
		input,
		auth,
		input.lineage.activeParentEntryID,
	)
	if err != nil {
		return nil, nil, err
	}

	runtime.emitContextCompactionEvent(
		ctx,
		input.onEvent,
		StreamEventContextCompactionDone,
		autoCompactionMessage(originalBudget, compactionEntry),
	)

	return build, compactionEntry, nil
}

func (runtime *Runtime) rebuildAfterPreRequestCompaction(
	ctx context.Context,
	input *completionRequestPreparationInput,
	auth model.RequestAuth,
	entryID string,
) (*contextRequestBuild, error) {
	build, err := runtime.buildCompletionRequest(ctx, &completionRequestBuildInput{
		selectedModel: input.selectedModel,
		auth:          auth,
		onEvent:       input.onEvent,
		sessionID:     input.sessionID,
		entryID:       entryID,
		cwd:           input.cwd,
		prompt:        input.prompt,
		lineage:       input.lineage,
	})
	if err != nil {
		runtime.emitContextCompactionError(ctx, input.onEvent, contextAutoCompactionBeforeRequestFailed, err)

		return nil, oops.In("assistant").
			Code("context_request_rebuild").
			Wrapf(err, "context: rebuild completion request after compaction")
	}

	runtime.emitUsageSnapshot(ctx, input.onEvent, &build.Context.Usage)

	if err := build.Budget.Validate(); err != nil {
		runtime.emitContextCompactionError(ctx, input.onEvent, contextAutoCompactionBeforeRequestFailed, err)

		return nil, oops.In("assistant").
			Code("context_budget_after_compact").
			Wrapf(err, "context: validate rebuilt budget")
	}

	return build, nil
}

func (runtime *Runtime) compactBeforeRequest(
	ctx context.Context,
	input *completionRequestPreparationInput,
	budget contextwindow.Budget,
	validationErr error,
) (*database.EntryEntity, error) {
	runtime.emitContextCompactionEvent(
		ctx,
		input.onEvent,
		StreamEventContextCompactionStart,
		preRequestAutoCompactionStartMessage(budget),
	)

	parentEntryID := input.lineage.activeParentEntryID

	entry, err := runtime.compactSessionFrom(ctx, input.sessionID, input.cwd, &parentEntryID, compaction.Operation{
		ID:     pendingCompactionOperationID,
		Reason: compaction.ReasonPreRequest, RetryIntent: compaction.RetryNone,
	})
	if isCompactNothingToDoError(err) {
		runtime.emitContextCompactionErrorMessage(
			ctx,
			input.onEvent,
			"context auto-compaction before request skipped: nothing to compact",
		)

		return nil, validationErr
	}

	if err != nil {
		runtime.emitContextCompactionError(ctx, input.onEvent, contextAutoCompactionBeforeRequestFailed, err)

		return nil, oops.In("assistant").
			Code("auto_compact").
			Wrapf(err, "auto-compact context before provider request")
	}

	return entry, nil
}

func isCompactNothingToDoError(err error) bool {
	code, ok := providerErrorCode(err)

	return ok && code == "compact_nothing_to_do"
}

func (runtime *Runtime) emitContextCompactionEvent(
	_ context.Context,
	onEvent func(StreamEvent),
	kind StreamEventKind,
	message string,
) {
	emitStreamEvent(onEvent, StreamEvent{ToolCallEvent: nil, ToolEvent: nil, Usage: nil, Kind: kind, Text: message})
}

func (runtime *Runtime) emitContextCompactionError(
	ctx context.Context,
	onEvent func(StreamEvent),
	prefix string,
	err error,
) {
	if err == nil {
		return
	}

	runtime.emitContextCompactionErrorMessage(ctx, onEvent, prefix+": "+err.Error())
}

func (runtime *Runtime) emitContextCompactionErrorMessage(
	ctx context.Context,
	onEvent func(StreamEvent),
	message string,
) {
	runtime.emitContextCompactionEvent(ctx, onEvent, StreamEventContextCompactionError, message)
}

type postResponseAutoCompactionInput struct {
	onEvent       func(StreamEvent)
	sessionID     string
	cwd           string
	parentEntryID string
}

func (runtime *Runtime) autoCompactAfterResponse(
	ctx context.Context,
	input *postResponseAutoCompactionInput,
) (model.TokenUsage, bool) {
	if !runtime.shouldTryPostResponseAutoCompaction(input) {
		return model.EmptyTokenUsage(), false
	}

	usage, err := runtime.ContextUsage(ctx, input.sessionID, input.cwd)
	if err != nil {
		runtime.emitPostResponseAutoCompactionError(ctx, input.onEvent, err)

		return model.EmptyTokenUsage(), false
	}

	budget := contextwindow.BudgetFromUsage(&usage)
	if !runtime.shouldAutoCompactAfterResponse(budget) {
		return model.EmptyTokenUsage(), false
	}

	runtime.emitContextCompactionEvent(
		ctx,
		input.onEvent,
		StreamEventContextCompactionStart,
		runtime.postResponseAutoCompactionStartMessage(budget),
	)

	operation := compaction.Operation{
		ID: pendingCompactionOperationID, Reason: compaction.ReasonPostResponse, RetryIntent: compaction.RetryNone,
	}

	entry, err := runtime.compactSessionFrom(
		ctx, input.sessionID, input.cwd, &input.parentEntryID, operation,
	)
	if isCompactNothingToDoError(err) {
		runtime.emitContextCompactionErrorMessage(
			ctx,
			input.onEvent,
			"context auto-compaction after response skipped: nothing to compact",
		)

		return model.EmptyTokenUsage(), false
	}

	if err != nil {
		runtime.emitPostResponseAutoCompactionError(ctx, input.onEvent, err)

		return model.EmptyTokenUsage(), false
	}

	compactedUsage, err := runtime.ContextUsage(ctx, input.sessionID, input.cwd)
	if err != nil {
		runtime.emitPostResponseAutoCompactionError(ctx, input.onEvent, err)

		return model.EmptyTokenUsage(), false
	}

	runtime.emitUsageSnapshot(ctx, input.onEvent, &compactedUsage)
	runtime.emitContextCompactionEvent(ctx, input.onEvent, StreamEventContextCompactionDone, compactionMessage(
		"context auto-compacted after response",
		budget,
		entry,
	))

	return compactedUsage, true
}

func (runtime *Runtime) shouldTryPostResponseAutoCompaction(input *postResponseAutoCompactionInput) bool {
	return runtime.cfg.Context.PreflightEnabled &&
		runtime.cfg.Context.AutoCompactionEnabled &&
		input != nil &&
		strings.TrimSpace(input.sessionID) != "" &&
		strings.TrimSpace(input.parentEntryID) != ""
}

func (runtime *Runtime) shouldAutoCompactAfterResponse(budget contextwindow.Budget) bool {
	return shouldAutoCompactAfterResponseAt(budget, runtime.cfg.Context.AutoCompactionThreshold)
}

func shouldAutoCompactAfterResponse(budget contextwindow.Budget) bool {
	return shouldAutoCompactAfterResponseAt(budget, postResponseAutoCompactThresholdPercent)
}

func shouldAutoCompactAfterResponseAt(budget contextwindow.Budget, threshold int) bool {
	if budget.ContextWindow <= 0 || budget.UsableInput <= 0 || budget.InputTokens <= 0 {
		return false
	}

	return budget.InputTokens >= budget.UsableInput*threshold/100
}

func (runtime *Runtime) emitPostResponseAutoCompactionError(
	ctx context.Context,
	onEvent func(StreamEvent),
	err error,
) {
	if err == nil {
		return
	}

	runtime.emitContextCompactionError(ctx, onEvent, "context auto-compaction after response failed", err)
}

func preRequestAutoCompactionStartMessage(budget contextwindow.Budget) string {
	message := "context auto-compacting before request: estimated input is %d tokens; usable input budget is %d"

	return fmt.Sprintf(message, budget.InputTokens, budget.UsableInput)
}

func (runtime *Runtime) postResponseAutoCompactionStartMessage(budget contextwindow.Budget) string {
	message := "context auto-compacting after response: estimated input is %d tokens; " +
		"threshold is %d%% of usable input budget %d"

	return fmt.Sprintf(message, budget.InputTokens, runtime.cfg.Context.AutoCompactionThreshold, budget.UsableInput)
}

func autoCompactionMessage(budget contextwindow.Budget, entry *database.EntryEntity) string {
	return compactionMessage("context auto-compacted before request", budget, entry)
}

func compactionMessage(prefix string, budget contextwindow.Budget, entry *database.EntryEntity) string {
	message := fmt.Sprintf(
		"%s: estimated input was %d tokens; usable input budget is %d tokens",
		prefix,
		budget.InputTokens,
		budget.UsableInput,
	)
	if entry == nil {
		return message
	}

	if entry.CompactionTokensBefore > 0 {
		message += fmt.Sprintf("; summarized %dk tokens", entry.CompactionTokensBefore/units.TokenThousand)
	}

	if entry.CompactionFirstKeptEntryID != "" {
		message += "; kept recent context from entry " + entry.CompactionFirstKeptEntryID
	}

	return message
}
