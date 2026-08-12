// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/samber/oops"
	retrylib "github.com/sethvargo/go-retry"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

type promptLineage struct {
	onEvent             func(StreamEvent)
	progress            *partialPromptProgress
	activeParentEntryID string
	runID               string
	pendingRounds       []llm.CompletedRound
	latestRoundUsage    model.TokenUsage
	committedThinking   int
	committedTools      int
	checkpointed        bool
}

type responseInput struct {
	lineage          *promptLineage
	onEvent          func(StreamEvent)
	onRetry          RetryEventHandler
	sessionID        string
	cwd              string
	prompt           string
	hasPromptImages  bool
	contextHasImages bool
}

func newPromptLineage(userEntryID string) *promptLineage {
	return newPromptLineageWithEvents(userEntryID, nil)
}

func newPromptLineageWithEvents(userEntryID string, onEvent func(StreamEvent)) *promptLineage {
	return &promptLineage{
		onEvent:             onEvent,
		activeParentEntryID: userEntryID,
		runID:               userEntryID,
		progress:            nil,
		pendingRounds:       nil,
		committedThinking:   0,
		committedTools:      0,
		latestRoundUsage:    model.EmptyTokenUsage(),
		checkpointed:        false,
	}
}

func (lineage *promptLineage) adopt(entry *database.EntryEntity) {
	if lineage != nil && entry != nil {
		lineage.activeParentEntryID = entry.ID
	}
}

func (runtime *Runtime) respond(
	ctx context.Context,
	input *responseInput,
) (
	bundle *responseBundle,
	cached bool,
	err error,
) {
	if strings.HasPrefix(strings.TrimSpace(input.prompt), slashPrefix) {
		slashResponse, slashToolEvents, slashErr := runtime.respondToSlashCommand(
			ctx,
			input.cwd,
			strings.TrimSpace(input.prompt),
			input.onEvent,
		)

		return &responseBundle{
			Text:          slashResponse,
			Thinking:      nil,
			ToolEvents:    slashToolEvents,
			Usage:         model.EmptyTokenUsage(),
			ProviderUsage: model.EmptyTokenUsage(),
			ModelFacing:   false,
		}, false, slashErr
	}

	cacheKey := runtime.cacheKey(input.sessionID, input.prompt)

	contextHasImages, contextErr := runtime.promptContextContainsImages(ctx, input.sessionID, input.lineage)
	if contextErr != nil {
		return nil, false, contextErr
	}

	if input.hasPromptImages || contextHasImages {
		return runtime.respondWithImages(ctx, input, contextHasImages)
	}

	cachedBundle, usable, cacheErr := runtime.cachedResponse(cacheKey, input)
	if cacheErr != nil {
		return nil, false, cacheErr
	}

	if usable {
		return cachedBundle, true, nil
	}

	input.contextHasImages = false

	bundle, err = runtime.modelResponse(ctx, input)
	if err != nil {
		return nil, false, err
	}

	if !input.lineage.checkpointed {
		runtime.cache.Set(cacheKey, bundle.Text)
	}

	return bundle, false, nil
}

func (runtime *Runtime) respondWithImages(
	ctx context.Context,
	input *responseInput,
	contextHasImages bool,
) (*responseBundle, bool, error) {
	// Image bytes deliberately stay out of cache keys; prompts with image-bearing
	// context always execute against their durable multipart history.
	input.contextHasImages = contextHasImages
	bundle, err := runtime.modelResponse(ctx, input)

	return bundle, false, err
}

func (runtime *Runtime) cachedResponse(cacheKey string, input *responseInput) (*responseBundle, bool, error) {
	cachedResponse, found, err := runtime.cache.Get(cacheKey)
	if err != nil {
		return nil, false, oops.In("assistant").Code("cache_get").Wrapf(err, "read response cache")
	}

	if !found {
		return nil, false, nil
	}

	return runtime.usableCachedResponse(input, cachedResponse)
}

func (runtime *Runtime) usableCachedResponse(
	input *responseInput,
	cachedResponse string,
) (*responseBundle, bool, error) {
	if runtime.steering == nil {
		return cachedResponseBundle(cachedResponse), true, nil
	}

	settled, err := runtime.steering.settleIfEmpty(input.sessionID, lineageRunID(input.lineage))
	if err != nil {
		return nil, false, oops.In("assistant").Code("steering_cache_settle").
			Wrapf(err, "settle steering before cached response")
	}

	if !settled {
		return nil, false, nil
	}

	return cachedResponseBundle(cachedResponse), true, nil
}

func cachedResponseBundle(cachedResponse string) *responseBundle {
	return &responseBundle{
		Text:          cachedResponse,
		Thinking:      nil,
		ToolEvents:    nil,
		Usage:         model.EmptyTokenUsage(),
		ProviderUsage: model.EmptyTokenUsage(),
		ModelFacing:   true,
	}
}

func (runtime *Runtime) modelResponse(
	ctx context.Context,
	input *responseInput,
) (*responseBundle, error) {
	if runtime.models == nil {
		return nil, oops.In("assistant").Code("models_unavailable").Errorf("model registry is not configured")
	}

	selectedModel, err := runtime.selectedModel()
	if err != nil {
		return nil, err
	}

	if input.contextHasImages {
		// Keep this early check: historical images are known before auth and context
		// construction, so an incompatible model should fail without doing either.
		imageErr := validateSelectedModelHasImageInput(&selectedModel, "conversation_history")
		if imageErr != nil {
			return nil, imageErr
		}
	}

	auth := runtime.models.RequestAuthContext(ctx, selectedModel.Provider)
	if !auth.OK {
		return nil, oops.In("assistant").
			Code("auth_missing").
			With("provider", selectedModel.Provider).
			Wrapf(fmt.Errorf("%s", auth.Error), "resolve model auth")
	}

	build, _, result, err := runtime.prepareAndComplete(ctx, input, &selectedModel, &auth)
	if err != nil {
		return nil, err
	}

	usage := contextwindow.MergeUsage(build.Context.Usage, result.Usage)
	runtime.emitUsage(ctx, input.onEvent, usage)

	thinking := result.Thinking
	toolEvents := result.ToolEvents
	providerUsage := result.Usage

	if input.lineage.checkpointed {
		thinking = suffixFrom(input.lineage.committedThinking, thinking)
		toolEvents = suffixFrom(input.lineage.committedTools, toolEvents)
		providerUsage = input.lineage.latestRoundUsage
	}

	return &responseBundle{
		Text:          result.Text,
		Thinking:      thinking,
		ToolEvents:    toolEvents,
		Usage:         usage,
		ProviderUsage: providerUsage,
		ModelFacing:   true,
	}, nil
}

func (runtime *Runtime) prepareAndComplete(
	ctx context.Context,
	input *responseInput,
	selectedModel *model.Model,
	auth *model.RequestAuth,
) (*contextRequestBuild, *database.EntryEntity, *CompletionResult, error) {
	preparation := &completionRequestPreparationInput{
		sessionID: input.sessionID, cwd: input.cwd, prompt: input.prompt, lineage: input.lineage,
		selectedModel: selectedModel, auth: auth, onEvent: input.onEvent,
	}

	build, compactionEntry, err := runtime.prepareCompletionRequestWithAutoCompaction(ctx, preparation)
	if err != nil {
		return nil, nil, nil, err
	}

	if err := validateModelContextImageInput(selectedModel, build.Request.Messages); err != nil {
		return nil, nil, nil, err
	}

	return runtime.completeWithProviderOverflowRecovery(ctx, &providerOverflowRecoveryInput{
		preparation: preparation, build: build, compactionEntry: compactionEntry, onRetry: input.onRetry,
	})
}

type modelCompletionRequestInput struct {
	selectedModel *model.Model
	registry      *tool.Registry
	onEvent       func(StreamEvent)
	lineage       *promptLineage
	sessionID     string
	systemPrompt  string
	cwd           string
	auth          model.RequestAuth
	messages      []database.MessageEntity
	usage         model.TokenUsage
}

func (runtime *Runtime) modelCompletionRequest(input *modelCompletionRequestInput) *CompletionRequest {
	request := &CompletionRequest{
		OnEvent:                input.onEvent,
		OnProviderObserve:      runtime.emitProviderRequest,
		OnProviderResponse:     observeProviderUsage,
		OnProviderRequest:      runtime.dispatchProviderRequestHook,
		OnRoundCheckpoint:      runtime.roundCheckpoint(input.sessionID, input.lineage),
		ToolRegistry:           input.registry,
		ExecuteTools:           nil,
		SessionID:              input.sessionID,
		SystemPrompt:           input.systemPrompt,
		ThinkingLevel:          runtime.thinkingLevel(),
		CWD:                    input.cwd,
		Auth:                   input.auth,
		Messages:               input.messages,
		Usage:                  input.usage,
		Model:                  *input.selectedModel,
		ProviderAttempt:        0,
		DisableTools:           false,
		ToolSideEffectsStarted: false,
	}
	executeTools := runtime.executeProviderToolCalls(input.registry, input.sessionID, input.cwd)
	request.ExecuteTools = func(
		ctx context.Context,
		calls []ToolCall,
		onEvent func(StreamEvent),
	) ([]ToolEvent, error) {
		request.ToolSideEffectsStarted = true

		return executeTools(ctx, calls, onEvent)
	}

	return request
}

func (runtime *Runtime) completeWithRetry(
	ctx context.Context,
	request *CompletionRequest,
	onRetry RetryEventHandler,
) (*CompletionResult, error) {
	retry := retryConfig(runtime.cfg)
	if !retry.Enabled || retry.MaxAttempts <= 1 {
		return runtime.completeAttempt(ctx, request, 1)
	}

	attempt := 0

	var result *CompletionResult

	var retryErr error

	backoff := retryBackoffWithOverride(retry, func(delay time.Duration) time.Duration {
		return providerRetryDelay(retryErr, delay)
	}, func(delay time.Duration) {
		retryEvent := RetryEvent{
			Kind:        RetryEventStart,
			Error:       "",
			Attempt:     attempt + 1,
			MaxAttempts: retry.MaxAttempts,
			Delay:       delay,
		}
		if retryErr != nil {
			retryEvent.Error = retryErr.Error()
		}

		runtime.emitRetryEvent(ctx, onRetry, retryEvent)
	})

	err := retrylib.Do(ctx, backoff, func(ctx context.Context) error {
		attempt++

		var err error

		result, err = runtime.retryAttempt(ctx, request, retry.MaxAttempts, attempt, onRetry)
		retryErr = retryError(err)

		return err
	})
	if err != nil {
		retryCanceled := errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
		retryCanceled = retryCanceled && (attempt == 0 || retryErr != nil && attempt < retry.MaxAttempts)

		return nil, retryResultError(err, retryCanceled)
	}

	return result, nil
}

func (runtime *Runtime) retryAttempt(
	ctx context.Context,
	request *CompletionRequest,
	maxAttempts int,
	attempt int,
	onRetry RetryEventHandler,
) (*CompletionResult, error) {
	result, err := runtime.completeAttempt(ctx, request, attempt)
	if err == nil {
		if attempt > 1 {
			runtime.emitRetryEvent(ctx, onRetry, RetryEvent{
				Kind:        RetryEventEnd,
				Error:       "",
				Attempt:     attempt,
				MaxAttempts: maxAttempts,
				Delay:       0,
			})
		}

		return result, nil
	}

	if request.ToolSideEffectsStarted || !ShouldRetryModelError(err) {
		return nil, err
	}

	return nil, retryableProviderError(err)
}

func retryError(err error) error {
	var retryFailed *retryFailedError
	if errors.As(err, &retryFailed) {
		return retryFailed.Unwrap()
	}

	if err == nil || !ShouldRetryModelError(err) {
		return nil
	}

	return err
}

func retryResultError(err error, retryCanceled bool) error {
	if retryCanceled {
		return oops.In("assistant").Code("retry_canceled").Wrapf(err, "wait before retry")
	}

	if retryErr := retryError(err); retryErr != nil {
		return retryErr
	}

	return err
}

func (runtime *Runtime) completeAttempt(
	ctx context.Context,
	request *CompletionRequest,
	attempt int,
) (*CompletionResult, error) {
	request.ProviderAttempt = attempt

	result, err := runtime.client.Complete(ctx, request)
	if err != nil {
		runtime.emitProviderError(ctx, request, attempt, err)

		return nil, assistantError(err, "complete model request")
	}

	runtime.emitProviderResponse(ctx, request, attempt, result)

	return result, nil
}

func suffixFrom[T any](committed int, values []T) []T {
	if committed <= 0 {
		return values
	}

	if committed >= len(values) {
		return nil
	}

	return values[committed:]
}

func (runtime *Runtime) emitRetryEvent(ctx context.Context, handler RetryEventHandler, retryEvent RetryEvent) {
	if handler != nil {
		handler(retryEvent)
	}

	if runtime.extensions == nil {
		return
	}

	if err := runtime.extensions.Emit(ctx, string(retryEvent.Kind), map[string]any{
		"attempt":      retryEvent.Attempt,
		"max_attempts": retryEvent.MaxAttempts,
		"delay_ms":     retryEvent.Delay.Milliseconds(),
		"error":        retryEvent.Error,
	}); err != nil && runtime.logger != nil {
		runtime.logger.Debug("extension retry event failed", "event", retryEvent.Kind, "error", err)
	}
}

func (runtime *Runtime) selectedModel() (model.Model, error) {
	provider := runtime.profile.Provider
	if provider == "" {
		provider = runtime.cfg.Assistant.Provider
	}

	modelID := runtime.profile.Model
	if modelID == "" {
		modelID = runtime.cfg.Assistant.Model
	}

	models := runtime.models.All()
	for index := range models {
		candidate := &models[index]
		if candidate.Provider == provider && candidate.ID == modelID {
			return *candidate, nil
		}
	}

	if provider == "" || modelID == "" {
		return model.Model{}, oops.In("assistant").Code("model_missing").Errorf("select a model with /model or /login")
	}

	return model.Model{
		ThinkingLevelMap: nil,
		Headers:          nil,
		Compat:           nil,
		Provider:         provider,
		ID:               modelID,
		Name:             modelID,
		API:              "openai-completions",
		BaseURL:          "",
		Input:            []model.InputMode{model.InputText},
		Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
		ContextWindow:    0,
		MaxTokens:        0,
		Reasoning:        false,
	}, nil
}

func (runtime *Runtime) thinkingLevel() string {
	if runtime.profile.ThinkingLevel != "" {
		return string(runtime.profile.ThinkingLevel)
	}

	return runtime.cfg.Assistant.ThinkingLevel
}

func (runtime *Runtime) promptContextContainsImages(
	ctx context.Context,
	sessionID string,
	lineage *promptLineage,
) (bool, error) {
	if lineage == nil || strings.TrimSpace(lineage.activeParentEntryID) == "" {
		return false, nil
	}

	hasImages, err := runtime.sessions.ContextHasImageParts(ctx, sessionID, lineage.activeParentEntryID)
	if err != nil {
		return false, oops.In("assistant").Code("load_image_context").
			Wrapf(err, "query session context for images")
	}

	return hasImages, nil
}

func (runtime *Runtime) cacheKey(sessionID, prompt string) string {
	selected, err := runtime.selectedModel()
	if err != nil {
		parts := []string{runtime.cfg.Assistant.Provider, runtime.cfg.Assistant.Model, sessionID, prompt}

		return strings.Join(parts, "\x00")
	}

	return strings.Join([]string{selected.Provider, selected.ID, sessionID, prompt}, "\x00")
}
