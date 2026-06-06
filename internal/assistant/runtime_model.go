// Package assistant orchestrates conversations, extensions, cache, and prompt execution.
package assistant

import (
	"context"
	"fmt"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

func (runtime *Runtime) respond(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	cwd string,
	prompt string,
	onEvent func(StreamEvent),
	onRetry RetryEventHandler,
) (
	bundle *responseBundle,
	cached bool,
	err error,
) {
	if strings.HasPrefix(prompt, slashPrefix) {
		slashResponse, slashToolEvents, slashErr := runtime.respondToSlashCommand(ctx, cwd, prompt, onEvent)
		return &responseBundle{
			ParentEntryID: nil,
			Text:          slashResponse,
			Thinking:      nil,
			ToolEvents:    slashToolEvents,
			Usage:         model.EmptyTokenUsage(),
			ModelFacing:   false,
		}, false, slashErr
	}

	cacheKey := runtime.cacheKey(sessionID, prompt)
	cachedResponse, found, err := runtime.cache.Get(cacheKey)
	if err != nil {
		return nil, false, oops.In("assistant").Code("cache_get").Wrapf(err, "read response cache")
	}
	if found {
		return &responseBundle{
			ParentEntryID: nil,
			Text:          cachedResponse,
			Thinking:      nil,
			ToolEvents:    nil,
			Usage:         model.EmptyTokenUsage(),
			ModelFacing:   true,
		}, true, nil
	}

	bundle, err = runtime.modelResponse(ctx, sessionID, userEntryID, cwd, prompt, onEvent, onRetry)
	if err != nil {
		return nil, false, err
	}
	runtime.cache.Set(cacheKey, bundle.Text)

	return bundle, false, nil
}

func (runtime *Runtime) modelResponse(
	ctx context.Context,
	sessionID string,
	userEntryID string,
	cwd string,
	prompt string,
	onEvent func(StreamEvent),
	onRetry RetryEventHandler,
) (*responseBundle, error) {
	if runtime.models == nil {
		return nil, oops.In("assistant").Code("models_unavailable").Errorf("model registry is not configured")
	}
	selectedModel, err := runtime.selectedModel()
	if err != nil {
		return nil, err
	}
	auth := runtime.models.RequestAuthContext(ctx, selectedModel.Provider)
	if !auth.OK {
		return nil, oops.In("assistant").
			Code("auth_missing").
			With("provider", selectedModel.Provider).
			Wrapf(fmt.Errorf("%s", auth.Error), "resolve model auth")
	}
	build, compactionEntry, err := runtime.prepareCompletionRequestWithAutoCompaction(
		ctx,
		&completionRequestPreparationInput{
			sessionID:     sessionID,
			cwd:           cwd,
			prompt:        prompt,
			userEntryID:   userEntryID,
			selectedModel: &selectedModel,
			auth:          &auth,
			onEvent:       onEvent,
		},
	)
	if err != nil {
		return nil, err
	}
	build, compactionEntry, result, err := runtime.completeWithProviderOverflowRecovery(
		ctx,
		&providerOverflowRecoveryInput{
			preparation: &completionRequestPreparationInput{
				sessionID:     sessionID,
				cwd:           cwd,
				prompt:        prompt,
				userEntryID:   userEntryID,
				selectedModel: &selectedModel,
				auth:          &auth,
				onEvent:       onEvent,
			},
			build:           build,
			compactionEntry: compactionEntry,
			onRetry:         onRetry,
		},
	)
	if err != nil {
		return nil, err
	}
	usage := mergeUsage(build.Context.Usage, result.Usage)
	runtime.emitUsage(ctx, onEvent, usage)

	parentEntryID := (*string)(nil)
	if compactionEntry != nil {
		parentEntryID = &compactionEntry.ID
	}

	return &responseBundle{
		ParentEntryID: parentEntryID,
		Text:          result.Text,
		Thinking:      result.Thinking,
		ToolEvents:    result.ToolEvents,
		Usage:         usage,
		ModelFacing:   true,
	}, nil
}

type modelCompletionRequestInput struct {
	selectedModel *model.Model
	registry      *tool.Registry
	onEvent       func(StreamEvent)
	sessionID     string
	systemPrompt  string
	cwd           string
	auth          model.RequestAuth
	messages      []database.MessageEntity
	usage         model.TokenUsage
}

func (runtime *Runtime) modelCompletionRequest(input *modelCompletionRequestInput) *CompletionRequest {
	return &CompletionRequest{
		OnEvent:           input.onEvent,
		OnProviderObserve: runtime.emitProviderRequest,
		OnProviderRequest: runtime.dispatchProviderRequestHook,
		OnToolCall:        runtime.dispatchToolCallLifecycle,
		OnToolResult:      runtime.dispatchToolResultLifecycle,
		ToolRegistry:      input.registry,
		SessionID:         input.sessionID,
		SystemPrompt:      input.systemPrompt,
		ThinkingLevel:     runtime.cfg.Assistant.ThinkingLevel,
		CWD:               input.cwd,
		Auth:              input.auth,
		Messages:          input.messages,
		Usage:             input.usage,
		Model:             *input.selectedModel,
		ProviderAttempt:   0,
		DisableTools:      false,
	}
}

func (runtime *Runtime) completeWithRetry(
	ctx context.Context,
	request *CompletionRequest,
	onRetry RetryEventHandler,
) (*CompletionResult, error) {
	retry := retryConfig(runtime.cfg)
	if !retry.Enabled || retry.MaxAttempts <= 1 {
		request.ProviderAttempt = 1
		result, err := runtime.client.Complete(ctx, request)
		if err != nil {
			runtime.emitProviderError(ctx, request, 1, err)
			return nil, err
		}
		runtime.emitProviderResponse(ctx, request, 1, result)
		return result, nil
	}

	var lastErr error
	for attempt := 1; attempt <= retry.MaxAttempts; attempt++ {
		request.ProviderAttempt = attempt
		result, err := runtime.client.Complete(ctx, request)
		if err == nil {
			runtime.emitProviderResponse(ctx, request, attempt, result)
			if attempt > 1 {
				runtime.emitRetryEvent(ctx, onRetry, RetryEvent{
					Kind:        RetryEventEnd,
					Error:       "",
					Attempt:     attempt,
					MaxAttempts: retry.MaxAttempts,
					Delay:       0,
				})
			}
			return result, nil
		}
		lastErr = err
		runtime.emitProviderError(ctx, request, attempt, err)
		if attempt == retry.MaxAttempts || !ShouldRetryModelError(err) {
			return nil, err
		}
		delay := retryDelay(attempt, retry)
		runtime.emitRetryEvent(ctx, onRetry, RetryEvent{
			Kind:        RetryEventStart,
			Attempt:     attempt + 1,
			MaxAttempts: retry.MaxAttempts,
			Delay:       delay,
			Error:       err.Error(),
		})
		if waitErr := waitForRetry(ctx, delay); waitErr != nil {
			return nil, oops.In("assistant").Code("retry_canceled").Wrapf(waitErr, "wait before retry")
		}
	}

	return nil, lastErr
}

func (runtime *Runtime) emitRetryEvent(ctx context.Context, handler RetryEventHandler, retryEvent RetryEvent) {
	if handler != nil {
		handler(retryEvent)
	}
	runtime.emit(ctx, string(retryEvent.Kind), retryEvent)
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
	provider := runtime.cfg.Assistant.Provider
	modelID := runtime.cfg.Assistant.Model
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

func (runtime *Runtime) cacheKey(sessionID, prompt string) string {
	return strings.Join(
		[]string{runtime.cfg.Assistant.Provider, runtime.cfg.Assistant.Model, sessionID, prompt},
		"\x00",
	)
}
