// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

const postResponseAutoCompactThresholdPercent = 80

type contextRequestBuild struct {
	Context *contextBuildResult
	Request *CompletionRequest
	Budget  contextBudget
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
	budget := newContextBudget(contextResult.Usage, selectedModel, runtime.cfg.Context, request)
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
		err := fmt.Errorf("nil completion request preparation input")

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
	validationErr := build.Budget.Validate()
	if validationErr == nil {
		return build, nil, nil
	}

	compactionEntry, err := runtime.CompactSessionFrom(ctx, input.sessionID, input.cwd, &input.userEntryID)
	if isCompactNothingToDoError(err) {
		return nil, nil, validationErr
	}
	if err != nil {
		return nil, nil, oops.In("assistant").
			Code("auto_compact").
			Wrapf(err, "auto-compact context before provider request")
	}
	runtime.emitContextCompaction(ctx, input.onEvent, autoCompactionMessage(build.Budget, compactionEntry))

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
		return nil, nil, oops.In("assistant").
			Code("context_request_rebuild").
			Wrapf(err, "context: rebuild completion request after compaction")
	}
	runtime.emitUsageSnapshot(ctx, input.onEvent, build.Context.Usage)
	if err := build.Budget.Validate(); err != nil {
		return nil, nil, oops.In("assistant").
			Code("context_budget_after_compact").
			Wrapf(err, "context: validate rebuilt budget")
	}

	return build, compactionEntry, nil
}

func isCompactNothingToDoError(err error) bool {
	code, ok := providerErrorCode(err)

	return ok && code == "compact_nothing_to_do"
}

func (runtime *Runtime) emitContextCompaction(ctx context.Context, onEvent func(StreamEvent), message string) {
	emitStreamEvent(onEvent, StreamEvent{
		ToolEvent: nil,
		Usage:     nil,
		Kind:      StreamEventContextCompaction,
		Text:      message,
	})
	runtime.emit(ctx, string(StreamEventContextCompaction), map[string]any{"message": message})
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
	budget := contextBudgetFromUsage(usage)
	if !shouldAutoCompactAfterResponse(budget) {
		return model.EmptyTokenUsage(), false
	}

	runtime.emitContextCompaction(ctx, input.onEvent, postResponseAutoCompactionStartMessage(budget))
	entry, err := runtime.CompactSessionFrom(ctx, input.sessionID, input.cwd, &input.parentEntryID)
	if isCompactNothingToDoError(err) {
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
	runtime.emitContextCompaction(ctx, input.onEvent, compactionMessage(
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

func contextBudgetFromUsage(usage model.TokenUsage) contextBudget {
	budget := contextBudget{
		InputTokens:       usage.ContextTokens,
		ContextWindow:     usage.ContextWindow,
		UsableInput:       usage.Breakdown["usable_input"],
		OutputReserve:     usage.Breakdown["reserve_output"],
		ToolSchemaReserve: usage.Breakdown["reserve_tools"],
		ProviderReserve:   usage.Breakdown["reserve_provider"],
		SafetyMargin:      usage.Breakdown["reserve_safety"],
	}

	return budget
}

func shouldAutoCompactAfterResponse(budget contextBudget) bool {
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
	runtime.emitContextCompaction(ctx, onEvent, "context auto-compaction after response failed: "+err.Error())
}

func postResponseAutoCompactionStartMessage(budget contextBudget) string {
	message := "context auto-compacting after response: estimated input is %d tokens; " +
		"threshold is %d%% of usable input budget %d"

	return fmt.Sprintf(message, budget.InputTokens, postResponseAutoCompactThresholdPercent, budget.UsableInput)
}

func autoCompactionMessage(budget contextBudget, entry *database.EntryEntity) string {
	return compactionMessage("context auto-compacted before request", budget, entry)
}

func compactionMessage(prefix string, budget contextBudget, entry *database.EntryEntity) string {
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
