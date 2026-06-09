package provider

import (
	"context"

	"github.com/samber/oops"
)

// HookInput describes a provider request before it is sent.
type HookInput struct {
	Request *CompletionRequest
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
		Request: request,
		Payload: cloneAnyMap(payload),
		Headers: cloneStringMap(headers),
		Attempt: providerAttempt(request),
	}
	if request == nil {
		return HookOutput{Payload: input.Payload, Headers: input.Headers}, nil
	}
	output := HookOutput{Payload: input.Payload, Headers: input.Headers}
	if request.OnProviderRequest != nil {
		mutated, err := request.OnProviderRequest(ctx, input)
		if err != nil {
			return HookOutput{}, oops.In("provider").
				Code("provider_request_hook_failed").
				Wrapf(err, "apply provider request hook")
		}
		output = mutated
	}
	if request.OnProviderObserve != nil {
		request.OnProviderObserve(ctx, request, providerAttempt(request))
	}

	return output, nil
}

func providerAttempt(request *CompletionRequest) int {
	if request == nil || request.ProviderAttempt <= 0 {
		return 1
	}

	return request.ProviderAttempt
}
