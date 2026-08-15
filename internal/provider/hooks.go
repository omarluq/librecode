package provider

import (
	"context"
	"errors"
	"fmt"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

// HookInput describes a provider request before it is sent.
type HookInput struct {
	Payload map[string]any
	Headers map[string]string
	Attempt int
}

// HookOutput describes the provider request after hook mutation.
type HookOutput struct {
	Payload map[string]any
	Headers map[string]string
}

func applyProviderRequestHook(
	ctx context.Context,
	request *CompletionRequest,
	payload map[string]any,
	headers map[string]string,
) (HookOutput, error) {
	input := HookInput{
		Payload: cloneAnyMap(payload),
		Headers: cloneStringMap(headers),
		Attempt: providerAttempt(request),
	}
	if request == nil {
		return HookOutput{Payload: input.Payload, Headers: input.Headers}, nil
	}

	output := HookOutput{Payload: input.Payload, Headers: input.Headers}
	if request.OnProviderRequest != nil {
		hookInput := hookInputToLLM(request, input.Payload, input.Headers, input.Attempt)

		mutated, err := request.OnProviderRequest(ctx, hookInput)
		if err != nil {
			return HookOutput{}, oops.In("provider").
				Code("provider_request_hook_failed").
				Wrapf(err, "apply provider request hook")
		}

		output = HookOutput{Payload: mutated.Payload, Headers: mutated.Headers}
	}

	if err := validateBoundedOutputMutation(request, input.Payload, output.Payload); err != nil {
		return HookOutput{}, oops.In("provider").Code("provider_request_hook_invalid_max_tokens").
			Wrapf(err, "validate provider request hook output bound")
	}

	if request.OnProviderObserve != nil {
		observeInput := hookInputToLLM(request, output.Payload, output.Headers, providerAttempt(request))
		request.OnProviderObserve(ctx, observeInput)
	}

	return output, nil
}

func validateBoundedOutputMutation(request *CompletionRequest, before, after map[string]any) error {
	if request == nil || request.Request.MaxTokens <= 0 {
		return nil
	}

	for _, key := range []string{"max_tokens", "max_completion_tokens", "max_output_tokens"} {
		if _, bounded := before[key]; !bounded {
			continue
		}

		if value, ok := integerPayloadValue(after[key]); !ok || value != int64(request.Request.MaxTokens) {
			return fmt.Errorf("bounded field %q must remain %d", key, request.Request.MaxTokens)
		}

		return nil
	}

	if request.Request.Model.API == apiOpenAICodexResponses {
		return nil
	}

	return errors.New("bounded request has no provider maximum-output field")
}

func integerPayloadValue(value any) (int64, bool) {
	switch typed := value.(type) {
	case int:
		return int64(typed), true
	case int64:
		return typed, true
	case float64:
		converted := int64(typed)

		return converted, float64(converted) == typed
	default:
		return 0, false
	}
}

func observeProviderResponse(ctx context.Context, request *CompletionRequest, usage *llm.Usage) {
	if request == nil || request.OnProviderResponse == nil {
		return
	}

	request.OnProviderResponse(ctx, *usage)
}

func providerAttempt(request *CompletionRequest) int {
	if request == nil || request.ProviderAttempt <= 0 {
		return 1
	}

	return request.ProviderAttempt
}

func hookInputToLLM(
	request *CompletionRequest,
	payload map[string]any,
	headers map[string]string,
	attempt int,
) *llm.HookInput {
	if request == nil {
		return &llm.HookInput{
			Model:           emptyModelRef(),
			ProviderOptions: nil,
			Payload:         cloneAnyMap(payload),
			Headers:         cloneStringMap(headers),
			SessionID:       "",
			ThinkingLevel:   "",
			MaxTokens:       0,
			Attempt:         attempt,
		}
	}

	return &llm.HookInput{
		Model:           request.Request.Model,
		ProviderOptions: cloneAnyMap(request.Request.ProviderOptions),
		Payload:         cloneAnyMap(payload),
		Headers:         cloneStringMap(headers),
		SessionID:       request.Request.SessionID,
		ThinkingLevel:   request.Request.ThinkingLevel,
		MaxTokens:       request.Request.MaxTokens,
		Attempt:         attempt,
	}
}

func emptyModelRef() llm.ModelRef {
	return llm.ModelRef{
		Metadata:         nil,
		ThinkingLevelMap: nil,
		Provider:         "",
		ID:               "",
		API:              "",
		BaseURL:          "",
		MaxTokens:        0,
		ContextWindow:    0,
		Reasoning:        false,
	}
}
