package assistant

import (
	"context"
	"maps"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/provider"
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

type providerHookInput = provider.HookInput

type providerHookOutput = provider.HookOutput

func (runtime *Runtime) dispatchProviderRequestHook(
	ctx context.Context,
	input providerHookInput,
) (providerHookOutput, error) {
	payload := providerRequestLifecyclePayload(input)
	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleBeforeProviderRequest, payload)
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

	return output, nil
}

func providerRequestLifecyclePayload(input providerHookInput) map[string]any {
	request := input.Request
	if request == nil {
		return map[string]any{}
	}

	return lifecyclepayload.ProviderRequestPayload(&lifecyclepayload.ProviderRequest{
		Payload:       input.Payload,
		Headers:       redactedHeaders(input.Headers),
		API:           request.Model.API,
		ModelID:       request.Model.ID,
		Provider:      request.Model.Provider,
		SessionID:     request.SessionID,
		ThinkingLevel: request.ThinkingLevel,
		Attempt:       input.Attempt,
	})
}

func providerPayloadFromLifecycle(payload, fallback map[string]any) map[string]any {
	if payload == nil {
		return cloneAnyMap(fallback)
	}
	mutated, ok := payload[lifecyclepayload.ProviderPayloadKey].(map[string]any)
	if !ok {
		return cloneAnyMap(fallback)
	}

	return cloneAnyMap(mutated)
}

func mergeProviderHeaders(headers, additions map[string]string) map[string]string {
	merged := cloneStringMap(headers)
	maps.Copy(merged, additions)

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
