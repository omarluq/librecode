package assistant

import (
	"context"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	providerRequestHeadersKey = "headers"
	providerRequestPayloadKey = "payload"
)

var blockedProviderHeaderNames = map[string]struct{}{
	"authorization":       {},
	"cookie":              {},
	"proxy-authorization": {},
	"x-api-key":           {},
	"api-key":             {},
}

// ProviderRequestHook can inspect and conservatively mutate a provider request.
type ProviderRequestHook func(context.Context, providerHookInput) (providerHookOutput, error)

type providerHookInput struct {
	Request *CompletionRequest
	Payload map[string]any
	Headers map[string]string
	Attempt int
}

type providerHookOutput struct {
	Payload map[string]any
	Headers map[string]string
}

func applyProviderRequestHook(
	ctx context.Context,
	request *CompletionRequest,
	payload map[string]any,
	headers map[string]string,
) (providerHookOutput, error) {
	input := providerHookInput{
		Request: request,
		Payload: cloneAnyMap(payload),
		Headers: cloneStringMap(headers),
		Attempt: providerAttempt(request),
	}
	if request == nil {
		return providerHookOutput{Payload: input.Payload, Headers: input.Headers}, nil
	}
	if request.OnProviderRequest != nil {
		output, err := request.OnProviderRequest(ctx, input)
		if err != nil {
			return providerHookOutput{}, oops.In("assistant").
				Code("provider_request_hook_failed").
				Wrapf(err, "apply provider request hook")
		}
		return output, nil
	}
	if request.OnProviderObserve != nil {
		request.OnProviderObserve(ctx, request, providerAttempt(request))
	}

	return providerHookOutput{Payload: input.Payload, Headers: input.Headers}, nil
}

func providerAttempt(request *CompletionRequest) int {
	if request == nil || request.ProviderAttempt <= 0 {
		return 1
	}

	return request.ProviderAttempt
}

func (runtime *Runtime) dispatchProviderRequestHook(
	ctx context.Context,
	input providerHookInput,
) (providerHookOutput, error) {
	payload := providerRequestLifecyclePayload(input)
	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleBeforeProviderRequest, payload)
	if err != nil {
		return providerHookOutput{}, oops.In("assistant").
			Code("before_provider_request_dispatch_failed").
			Wrapf(err, "dispatch before_provider_request lifecycle")
	}
	if err := validateProviderRequestMutation(result.ProviderRequest); err != nil {
		return providerHookOutput{}, oops.In("assistant").
			Code("provider_request_mutation_invalid").
			Wrapf(err, "validate provider_request mutation")
	}

	output := providerHookOutput{
		Payload: providerPayloadFromLifecycle(result.Payload, input.Payload),
		Headers: mergeProviderHeaders(input.Headers, result.ProviderRequest.Headers),
	}
	runtime.emitLifecycleDiagnostics(
		ctx,
		extension.LifecycleBeforeProviderRequest,
		&result,
		providerHookDiagnostics(input, output),
	)

	return output, nil
}

func providerRequestLifecyclePayload(input providerHookInput) map[string]any {
	request := input.Request
	if request == nil {
		return map[string]any{}
	}

	return map[string]any{
		lifecycleAPIKey:           request.Model.API,
		lifecycleAttemptKey:       input.Attempt,
		providerRequestHeadersKey: redactedHeaders(input.Headers),
		jsonModelKey:              request.Model.ID,
		lifecycleProviderKey:      request.Model.Provider,
		jsonSessionIDKey:          request.SessionID,
		providerRequestPayloadKey: cloneAnyMap(input.Payload),
		"thinking_level":          request.ThinkingLevel,
	}
}

func providerPayloadFromLifecycle(payload, fallback map[string]any) map[string]any {
	if payload == nil {
		return cloneAnyMap(fallback)
	}
	mutated, ok := payload[providerRequestPayloadKey].(map[string]any)
	if !ok {
		return cloneAnyMap(fallback)
	}

	return cloneAnyMap(mutated)
}

func mergeProviderHeaders(headers, additions map[string]string) map[string]string {
	merged := cloneStringMap(headers)
	for key, value := range additions {
		merged[key] = value
	}

	return merged
}

func validateProviderRequestMutation(mutation extension.ProviderRequestMutation) error {
	for header := range mutation.Headers {
		if _, blocked := blockedProviderHeaderNames[strings.ToLower(strings.TrimSpace(header))]; blocked {
			return oops.In("assistant").
				Code("provider_hook_header_blocked").
				With("header", header).
				Errorf("provider hook cannot mutate sensitive header")
		}
	}

	return nil
}

func redactedHeaders(headers map[string]string) map[string]string {
	redacted := make(map[string]string, len(headers))
	for key, value := range headers {
		if _, blocked := blockedProviderHeaderNames[strings.ToLower(strings.TrimSpace(key))]; blocked {
			redacted[key] = "[redacted]"
			continue
		}
		redacted[key] = value
	}

	return redacted
}

func cloneStringMap(values map[string]string) map[string]string {
	if values == nil {
		return map[string]string{}
	}
	cloned := make(map[string]string, len(values))
	for key, value := range values {
		cloned[key] = value
	}

	return cloned
}
