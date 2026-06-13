package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/extension"
)

func (runtime *Runtime) emitProviderRequest(ctx context.Context, request *CompletionRequest, attempt int) {
	if request == nil {
		return
	}

	payload := new(lifecyclepayload.ProviderRequest)
	payload.Headers = redactedHeaders(request.Auth.Headers)
	payload.API = request.Model.API
	payload.ModelID = request.Model.ID
	payload.Provider = request.Model.Provider
	payload.SessionID = request.SessionID
	payload.ThinkingLevel = request.ThinkingLevel
	payload.Attempt = attempt

	runtime.emit(
		ctx,
		string(extension.LifecycleBeforeProviderRequest),
		lifecyclepayload.ProviderRequestPayload(payload),
	)
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

	payload := lifecyclepayload.ProviderResponsePayload(&lifecyclepayload.ProviderResponse{
		FinishReason:   result.FinishReason,
		Usage:          result.Usage,
		API:            request.Model.API,
		ModelID:        request.Model.ID,
		Provider:       request.Model.Provider,
		SessionID:      request.SessionID,
		Text:           result.Text,
		Attempt:        attempt,
		ThinkingCount:  len(result.Thinking),
		ToolEventCount: len(result.ToolEvents),
	})

	dispatchResult, dispatchErr := runtime.dispatchLifecycle(ctx, extension.LifecycleAfterProviderResponse, payload)
	if dispatchErr != nil && runtime.logger != nil {
		runtime.logger.Debug("provider response lifecycle failed", "error", dispatchErr)
	}

	runtime.emitLifecycleDiagnostics(
		ctx,
		extension.LifecycleAfterProviderResponse,
		&dispatchResult,
		providerResponseDiagnostics(request, attempt, result),
	)
}

func (runtime *Runtime) emitProviderError(ctx context.Context, request *CompletionRequest, attempt int, err error) {
	if request == nil || err == nil {
		return
	}

	payload := lifecyclepayload.ProviderErrorPayload(&lifecyclepayload.ProviderError{
		Err:       err,
		API:       request.Model.API,
		ModelID:   request.Model.ID,
		Provider:  request.Model.Provider,
		SessionID: request.SessionID,
		Attempt:   attempt,
	})

	dispatchResult, dispatchErr := runtime.dispatchLifecycle(ctx, extension.LifecycleProviderError, payload)
	if dispatchErr != nil && runtime.logger != nil {
		runtime.logger.Debug("provider error lifecycle failed", "error", dispatchErr)
	}

	runtime.emitLifecycleDiagnostics(
		ctx,
		extension.LifecycleProviderError,
		&dispatchResult,
		providerErrorDiagnostics(request, attempt, err),
	)
}
