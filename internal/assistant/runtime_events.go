package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	lifecycleAPIKey       = "api"
	lifecycleAttemptKey   = "attempt"
	lifecycleProviderKey  = "provider"
	lifecycleToolErrorKey = "error"
)

func (runtime *Runtime) emitProviderRequest(ctx context.Context, request *CompletionRequest, attempt int) {
	if request == nil {
		return
	}
	runtime.emit(ctx, string(extension.LifecycleBeforeProviderRequest), map[string]any{
		lifecycleAPIKey:           request.Model.API,
		lifecycleAttemptKey:       attempt,
		providerRequestHeadersKey: redactedHeaders(request.Auth.Headers),
		jsonModelKey:              request.Model.ID,
		lifecycleProviderKey:      request.Model.Provider,
		jsonSessionIDKey:          request.SessionID,
		"thinking_level":          request.ThinkingLevel,
	})
}

func (runtime *Runtime) emitProviderResponse(
	ctx context.Context,
	request *CompletionRequest,
	attempt int,
	result *CompletionResult,
) {
	if request == nil || result == nil {
		return
	}
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleAfterProviderResponse, map[string]any{
		lifecycleAPIKey:      request.Model.API,
		lifecycleAttemptKey:  attempt,
		jsonModelKey:         request.Model.ID,
		lifecycleProviderKey: request.Model.Provider,
		jsonSessionIDKey:     request.SessionID,
		jsonTextKey:          result.Text,
		"thinking_count":     len(result.Thinking),
		"tool_event_count":   len(result.ToolEvents),
		jsonUsageKey:         tokenUsageLifecyclePayload(result.Usage),
	})
}

func (runtime *Runtime) emitProviderError(ctx context.Context, request *CompletionRequest, attempt int, err error) {
	if request == nil || err == nil {
		return
	}
	runtime.dispatchObservationalLifecycle(ctx, extension.LifecycleProviderError, map[string]any{
		lifecycleAPIKey:      request.Model.API,
		lifecycleAttemptKey:  attempt,
		jsonModelKey:         request.Model.ID,
		lifecycleProviderKey: request.Model.Provider,
		jsonSessionIDKey:     request.SessionID,
		lifecycleErrorKey:    err.Error(),
	})
}

func toolCallPayload(call ToolCallEvent) map[string]any {
	return map[string]any{
		"call_id":        call.ID,
		jsonToolNameKey:  call.Name,
		"arguments_json": call.ArgumentsJSON,
		"arguments":      call.Arguments,
	}
}

func toolEventPayload(event *ToolEvent) map[string]any {
	return map[string]any{
		jsonToolNameKey:       event.Name,
		"arguments_json":      event.ArgumentsJSON,
		"details_json":        event.DetailsJSON,
		"result":              event.Result,
		lifecycleToolErrorKey: event.Error,
	}
}
