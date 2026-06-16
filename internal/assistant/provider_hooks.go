package assistant

import (
	"context"
	"maps"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/assistant/lifecyclepayload"
	"github.com/omarluq/librecode/internal/extension"
	"github.com/omarluq/librecode/internal/llm"
)

func blockedProviderHeaderNames() map[string]struct{} {
	return map[string]struct{}{
		"authorization":       {},
		"cookie":              {},
		"proxy-authorization": {},
		"x-api-key":           {},
		"api-key":             {},
	}
}

func (runtime *Runtime) dispatchProviderRequestHook(
	ctx context.Context,
	input *llm.HookInput,
) (llm.HookOutput, error) {
	if input == nil {
		return llm.HookOutput{Payload: nil, Headers: nil}, nil
	}

	payload := providerRequestLifecyclePayload(input)
	result, err := runtime.dispatchLifecycle(ctx, extension.LifecycleBeforeProviderRequest, payload)
	output := llm.HookOutput{
		Payload: providerPayloadFromLifecycle(result.Payload, input.Payload),
		Headers: mergeProviderHeaders(input.Headers, result.ProviderRequest.Headers),
	}

	if err != nil {
		return llm.HookOutput{}, oops.In("assistant").
			Code("before_provider_request_dispatch_failed").
			Wrapf(err, "dispatch before_provider_request lifecycle")
	}

	if err := validateProviderRequestMutation(result.ProviderRequest); err != nil {
		return llm.HookOutput{}, oops.In("assistant").
			Code("provider_request_mutation_invalid").
			Wrapf(err, "validate provider_request mutation")
	}

	return output, nil
}

func providerRequestLifecyclePayload(input *llm.HookInput) map[string]any {
	if input == nil {
		return map[string]any{}
	}

	return lifecyclepayload.ProviderRequestPayload(&lifecyclepayload.ProviderRequest{
		Payload:       input.Payload,
		Headers:       redactedHeaders(input.Headers),
		API:           input.Model.API,
		ModelID:       input.Model.ID,
		Provider:      input.Model.Provider,
		SessionID:     input.SessionID,
		ThinkingLevel: input.ThinkingLevel,
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
		if _, blocked := blockedProviderHeaderNames()[strings.ToLower(strings.TrimSpace(header))]; blocked {
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
		if _, blocked := blockedProviderHeaderNames()[strings.ToLower(strings.TrimSpace(key))]; blocked {
			redacted[key] = "[redacted]"

			continue
		}

		redacted[key] = value
	}

	return redacted
}
