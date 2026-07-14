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

	preparation := &completionRequestPreparationInput{
		sessionID:     sessionID,
		cwd:           cwd,
		prompt:        prompt,
		userEntryID:   userEntryID,
		selectedModel: &selectedModel,
		auth:          &auth,
		onEvent:       onEvent,
	}

	build, compactionEntry, err := runtime.prepareCompletionRequestWithAutoCompaction(ctx, preparation)
	if err != nil {
		return nil, err
	}

	build, compactionEntry, result, err := runtime.completeWithProviderOverflowRecovery(
		ctx,
		&providerOverflowRecoveryInput{
			preparation:     preparation,
			build:           build,
			compactionEntry: compactionEntry,
			onRetry:         onRetry,
		},
	)
	if err != nil {
		return nil, err
	}

	usage := contextwindow.MergeUsage(build.Context.Usage, result.Usage)
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
		ToolRegistry:      input.registry,
		ExecuteTools:      runtime.executeProviderToolCalls(input.registry),
		SessionID:         input.sessionID,
		SystemPrompt:      input.systemPrompt,
		ThinkingLevel:     runtime.thinkingLevel(),
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
		return runtime.completeAttempt(ctx, request, 1)
	}

	attempt := 0

	var result *CompletionResult

	var retryErr error

	backoff := retryBackoff(retry, func(delay time.Duration) {
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

	if !ShouldRetryModelError(err) {
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

func (runtime *Runtime) cacheKey(sessionID, prompt string) string {
	selected, err := runtime.selectedModel()
	if err != nil {
		parts := []string{runtime.cfg.Assistant.Provider, runtime.cfg.Assistant.Model, sessionID, prompt}

		return strings.Join(parts, "\x00")
	}

	return strings.Join([]string{selected.Provider, selected.ID, sessionID, prompt}, "\x00")
}
