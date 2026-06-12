// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
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

func (runtime *Runtime) buildCompletionRequest(
	ctx context.Context,
	sessionID string,
	cwd string,
	prompt string,
	selectedModel *model.Model,
	auth model.RequestAuth,
	onEvent func(StreamEvent),
) (*contextRequestBuild, error) {
	contextResult, err := runtime.buildModelContext(ctx, sessionID, cwd, prompt, selectedModel, onEvent)
	if err != nil {
		return nil, oops.In("assistant").Code("context_build_model").Wrapf(err, "context: build model context")
	}
	registry, err := newToolRegistry(cwd, runtime.extensions)
	if err != nil {
		return nil, oops.In("assistant").Code("context_tool_registry").Wrapf(err, "context: create tool registry")
	}
	request := runtime.modelCompletionRequest(&modelCompletionRequestInput{
		selectedModel: selectedModel,
		registry:      registry,
		onEvent:       onEvent,
		messages:      contextResult.Messages,
		auth:          auth,
		usage:         contextResult.Usage,
		sessionID:     sessionID,
		systemPrompt:  contextResult.SystemPrompt,
		cwd:           cwd,
	})
	budget := contextwindow.NewBudget(
		contextResult.Usage,
		selectedModel,
		runtime.cfg.Context,
		func() int { return estimateToolSchemaTokens(request) },
	)
	contextResult.Usage = budget.UsageWithBudget(contextResult.Usage)
	request.Usage = contextResult.Usage

	return &contextRequestBuild{Context: contextResult, Request: request, Budget: budget}, nil
}

type completionRequestPreparationInput struct {
	selectedModel *model.Model
	onEvent       func(StreamEvent)
	auth          *model.RequestAuth
	sessionID     string
	cwd           string
	prompt        string
	userEntryID   string
}

func (runtime *Runtime) prepareCompletionRequestWithAutoCompaction(
	ctx context.Context,
	input *completionRequestPreparationInput,
) (*contextRequestBuild, *database.EntryEntity, error) {
	if input == nil || input.auth == nil {
		err := errors.New("nil completion request preparation input")

		return nil, nil, oops.In("assistant").
			Code("context_prepare_input").
			Wrapf(err, "context: invalid completion preparation input")
	}
	auth := *input.auth

	build, err := runtime.buildCompletionRequest(
		ctx,
		input.sessionID,
		input.cwd,
		input.prompt,
		input.selectedModel,
		auth,
		input.onEvent,
	)
	if err != nil {
		return nil, nil, oops.In("assistant").
			Code("context_request_build").
			Wrapf(err, "context: build completion request")
	}
	runtime.emitUsage(ctx, input.onEvent, build.Context.Usage)
	if !runtime.cfg.Context.PreflightEnabled {
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

	build, err = runtime.buildCompletionRequest(
		ctx,
		input.sessionID,
		input.cwd,
		input.prompt,
		input.selectedModel,
		auth,
		input.onEvent,
	)
	if err != nil {
		runtime.emitContextCompactionError(ctx, input.onEvent, contextAutoCompactionBeforeRequestFailed, err)

		return nil, nil, oops.In("assistant").
			Code("context_request_rebuild").
			Wrapf(err, "context: rebuild completion request after compaction")
	}
	runtime.emitUsageSnapshot(ctx, input.onEvent, build.Context.Usage)
	if err := build.Budget.Validate(); err != nil {
		runtime.emitContextCompactionError(ctx, input.onEvent, contextAutoCompactionBeforeRequestFailed, err)

		return nil, nil, oops.In("assistant").
			Code("context_budget_after_compact").
			Wrapf(err, "context: validate rebuilt budget")
	}
	runtime.emitContextCompactionEvent(
		ctx,
		input.onEvent,
		StreamEventContextCompactionDone,
		autoCompactionMessage(originalBudget, compactionEntry),
	)

	return build, compactionEntry, nil
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
	entry, err := runtime.CompactSessionFrom(ctx, input.sessionID, input.cwd, &input.userEntryID)
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
	ctx context.Context,
	onEvent func(StreamEvent),
	kind StreamEventKind,
	message string,
) {
	emitStreamEvent(onEvent, StreamEvent{ToolEvent: nil, Usage: nil, Kind: kind, Text: message})
	runtime.emit(ctx, string(kind), map[string]any{"message": message})
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
	budget := contextwindow.BudgetFromUsage(usage)
	if !shouldAutoCompactAfterResponse(budget) {
		return model.EmptyTokenUsage(), false
	}

	runtime.emitContextCompactionEvent(
		ctx,
		input.onEvent,
		StreamEventContextCompactionStart,
		postResponseAutoCompactionStartMessage(budget),
	)
	entry, err := runtime.CompactSessionFrom(ctx, input.sessionID, input.cwd, &input.parentEntryID)
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
	runtime.emitUsageSnapshot(ctx, input.onEvent, compactedUsage)
	runtime.emitContextCompactionEvent(ctx, input.onEvent, StreamEventContextCompactionDone, compactionMessage(
		"context auto-compacted after response",
		budget,
		entry,
	))

	return compactedUsage, true
}

func (runtime *Runtime) shouldTryPostResponseAutoCompaction(input *postResponseAutoCompactionInput) bool {
	return runtime.cfg.Context.PreflightEnabled && input != nil && strings.TrimSpace(input.sessionID) != "" &&
		strings.TrimSpace(input.parentEntryID) != ""
}

func shouldAutoCompactAfterResponse(budget contextwindow.Budget) bool {
	if budget.ContextWindow <= 0 || budget.UsableInput <= 0 || budget.InputTokens <= 0 {
		return false
	}

	return budget.InputTokens >= budget.UsableInput*postResponseAutoCompactThresholdPercent/100
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

func postResponseAutoCompactionStartMessage(budget contextwindow.Budget) string {
	message := "context auto-compacting after response: estimated input is %d tokens; " +
		"threshold is %d%% of usable input budget %d"

	return fmt.Sprintf(message, budget.InputTokens, postResponseAutoCompactThresholdPercent, budget.UsableInput)
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
		message += fmt.Sprintf("; summarized %dk tokens", entry.CompactionTokensBefore/1000)
	}
	if entry.CompactionFirstKeptEntryID != "" {
		message += "; kept recent context from entry " + entry.CompactionFirstKeptEntryID
	}

	return message
}
