package provider

import (
	"context"

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
	if request.OnProviderObserve != nil {
		observeInput := hookInputToLLM(request, output.Payload, output.Headers, providerAttempt(request))
		request.OnProviderObserve(ctx, observeInput)
	}

	return output, nil
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
