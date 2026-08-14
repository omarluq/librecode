package provider

import (
	"context"
	"io"
	"strings"

	"github.com/omarluq/librecode/internal/llm"
)

func (client *HTTPCompletionClient) completeOpenAIResponses(
	ctx context.Context,
	request *CompletionRequest,
) (*llm.Response, error) {
	input, err := openAIResponseInput(request.Request.Messages)
	if err != nil {
		return nil, err
	}

	endpoint := joinEndpoint(request.Request.Model.BaseURL, "/responses")

	return client.completeResponsesLoop(ctx, request, endpoint, openAIHeaders(request), input)
}

func (client *HTTPCompletionClient) completeOpenAICodex(
	ctx context.Context,
	request *CompletionRequest,
) (*llm.Response, error) {
	messages, err := compactCodexResponseMessages(request.Request.Messages)
	if err != nil {
		return nil, err
	}

	input, err := openAIResponseInput(messages)
	if err != nil {
		return nil, err
	}

	endpoint := joinEndpoint(request.Request.Model.BaseURL, "/codex/responses")

	return client.completeResponsesLoop(ctx, request, endpoint, codexHeaders(request), input)
}

func compactCodexResponseMessages(messages []llm.Message) ([]llm.Message, error) {
	if err := validateImageMessages(messages); err != nil {
		return nil, err
	}

	return compactResponseMessages(messages), nil
}

func (client *HTTPCompletionClient) completeResponsesLoop(
	ctx context.Context,
	request *CompletionRequest,
	endpoint string,
	headers map[string]string,
	input []any,
) (*llm.Response, error) {
	result := newResponse()

	for {
		payload := client.responsesPayload(request, input)

		providerRequest, err := applyProviderRequestHook(ctx, request, payload, cloneStringMap(headers))
		if err != nil {
			return nil, err
		}

		providerResult, err := client.responsesRound(ctx, request, endpoint, providerRequest)
		if err != nil {
			return nil, err
		}

		observeProviderResponse(ctx, request, &providerResult.Usage)
		appendThinking(result, providerResult.Thinking)
		result.Usage = accumulateUsage(&result.Usage, &providerResult.Usage)
		result.FinishReason = providerResult.FinishReason

		if len(providerResult.ToolCalls) == 0 {
			finished, nextInput, finishErr := advanceResponsesTextRound(ctx, request, result, input, providerResult)
			if finishErr != nil {
				return nil, finishErr
			}

			if finished {
				return result, nil
			}

			input = nextInput

			continue
		}

		input, err = advanceResponsesToolRound(ctx, request, result, input, providerResult)
		if err != nil {
			return nil, err
		}
	}
}

func (client *HTTPCompletionClient) responsesRound(
	ctx context.Context,
	request *CompletionRequest,
	endpoint string,
	providerRequest HookOutput,
) (*providerResult, error) {
	result, err := client.requestResponses(
		ctx,
		endpoint,
		providerRequest.Headers,
		providerRequest.Payload,
		request.OnEvent,
	)
	if err != nil {
		return nil, err
	}

	if err := validateToolDispatch(result.FinishReason, result.ToolCalls); err != nil {
		return nil, err
	}

	return result, nil
}

func advanceResponsesTextRound(
	ctx context.Context,
	request *CompletionRequest,
	result *llm.Response,
	input []any,
	providerResult *providerResult,
) (finished bool, nextInput []any, err error) {
	steering, err := runRoundCheckpoint(ctx, request, new(completedRound(providerResult, nil)))
	if err != nil {
		return false, nil, err
	}

	if len(steering) > 0 {
		return false, appendResponsesRound(input, providerResult, steering), nil
	}

	setResponseText(result, providerResult.Text)
	result.FinishReason = normalizedFinishReason(providerResult)

	return true, input, nil
}

func advanceResponsesToolRound(
	ctx context.Context,
	request *CompletionRequest,
	result *llm.Response,
	input []any,
	providerResult *providerResult,
) ([]any, error) {
	input = append(input, statelessResponseOutputItems(providerResult.OutputItems)...)

	outputs, events, err := executeToolCalls(ctx, request, providerResult.ToolCalls)
	if err != nil {
		return nil, err
	}

	appendToolResults(result, events)

	input = append(input, outputs...)

	steering, err := runRoundCheckpoint(ctx, request, new(completedRoundWithToolEvents(providerResult, events)))
	if err != nil {
		return nil, err
	}

	return appendOpenAIResponseUserMessages(input, steering), nil
}

func appendResponsesRound(
	input []any,
	result *providerResult,
	steering []llm.Message,
) []any {
	input = append(input, statelessResponseOutputItems(result.OutputItems)...)
	if text := strings.TrimSpace(result.Text); text != "" {
		input = append(input, map[string]any{
			jsonRoleKey: jsonAssistantRole,
			jsonContentKey: []map[string]any{{
				jsonTypeKey: "output_text",
				jsonTextKey: text,
			}},
		})
	}

	return appendOpenAIResponseUserMessages(input, steering)
}

func appendOpenAIResponseUserMessages(input []any, messages []llm.Message) []any {
	for index := range messages {
		input = append(input, map[string]any{
			jsonRoleKey:    jsonUserRole,
			jsonContentKey: openAIResponseUserContent(messages[index]),
		})
	}

	return input
}

func (client *HTTPCompletionClient) responsesPayload(request *CompletionRequest, input []any) map[string]any {
	return responsesPayload(request, input)
}

func responsesPayload(request *CompletionRequest, input []any) map[string]any {
	payload := responsesBasePayload(request, input)
	payload[jsonToolsKey] = responseTools(requestToolDefinitions(request))
	payload[jsonToolChoiceKey] = "auto"
	payload["parallel_tool_calls"] = true

	return payload
}

func responsesBasePayload(request *CompletionRequest, input []any) map[string]any {
	payload := map[string]any{
		jsonModelKey:  request.Request.Model.ID,
		"store":       false,
		jsonStreamKey: true,
		jsonInputKey:  input,
	}
	if request.Request.SystemPrompt != "" {
		payload["instructions"] = request.Request.SystemPrompt
	}

	if request.Request.MaxTokens > 0 {
		payload["max_output_tokens"] = request.Request.MaxTokens
	}

	payload["text"] = map[string]string{"verbosity": "low"}
	payload["include"] = []string{reasoningContentKey}
	payload["prompt_cache_key"] = request.Request.SessionID

	if effort, ok := reasoningEffort(request); ok {
		payload[jsonReasoningKey] = map[string]any{
			reasoningEffortKey: effort,
			jsonSummaryKey:     reasoningSummaryAuto,
		}
	} else {
		payload[jsonReasoningKey] = codexReasoning(request)
	}

	return payload
}

func (client *HTTPCompletionClient) requestResponses(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
	onEvent func(*llm.StreamChunk),
) (*providerResult, error) {
	return client.requestProviderStream(
		ctx,
		endpoint,
		headers,
		payload,
		func(reader io.Reader) (*providerResult, error) {
			return parseSSEResult(reader, onEvent)
		},
	)
}

func statelessResponseOutputItems(items []any) []any {
	stateless := make([]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object[jsonTypeKey]) != functionCallType {
			continue
		}

		stateless = append(stateless, map[string]any{
			jsonTypeKey:      functionCallType,
			jsonCallIDKey:    stringValue(object[jsonCallIDKey]),
			jsonToolNameKey:  stringValue(object[jsonToolNameKey]),
			jsonArgumentsKey: stringValue(object[jsonArgumentsKey]),
			"status":         statusCompleted,
		})
	}

	return stateless
}

func providerResultFromResponse(response map[string]any) *providerResult {
	outputItems := outputItemsFromResponse(response[jsonOutputKey])
	toolCalls := toolCallsFromOutput(outputItems)

	text := strings.TrimSpace(extractText(response[jsonOutputKey]))
	if text == "" {
		if outputText, ok := response["output_text"].(string); ok {
			text = strings.TrimSpace(outputText)
		}
	}

	return &providerResult{
		FinishReason: openAIResponseFinishReason(response, len(toolCalls) > 0),
		Termination:  openAIResponseTermination(response),
		Text:         text,
		OutputItems:  outputItems,
		Thinking:     thinkingFromOutput(outputItems),
		ToolCalls:    toolCalls,
		Usage:        usageFromObject(response["usage"]),
	}
}

func providerResultFromOutputItems(outputItems []any, fallbackText string) *providerResult {
	text := strings.TrimSpace(extractText(outputItems))
	if text == "" {
		text = fallbackText
	}

	toolCalls := toolCallsFromOutput(outputItems)

	return &providerResult{
		FinishReason: openAIResponseOutputItemsFinishReason(toolCalls),
		Termination: llm.TerminationMetadata{
			ProviderStatus:       "",
			ProviderFinishReason: "",
			IncompleteReason:     "",
		},
		Text:        text,
		OutputItems: outputItems,
		Thinking:    thinkingFromOutput(outputItems),
		ToolCalls:   toolCalls,
		Usage:       llm.EmptyUsage(),
	}
}

func openAIResponseFinishReason(response map[string]any, hasToolCalls bool) llm.FinishReason {
	status := stringValue(response["status"])
	if status == statusCompleted {
		return llm.FinishReasonStop
	}

	if status == "failed" {
		return llm.FinishReasonError
	}

	if status != "incomplete" {
		if hasToolCalls {
			return llm.FinishReasonToolCalls
		}

		return llm.FinishReasonUnknown
	}

	return openAIIncompleteFinishReason(response)
}

func openAIResponseTermination(response map[string]any) llm.TerminationMetadata {
	incomplete := ""
	if details, ok := response["incomplete_details"].(map[string]any); ok {
		incomplete = stringValue(details["reason"])
	}

	return llm.NewTerminationMetadata(stringValue(response["status"]), "", incomplete)
}

func openAIIncompleteFinishReason(response map[string]any) llm.FinishReason {
	details, ok := response["incomplete_details"].(map[string]any)
	if !ok {
		return llm.FinishReasonError
	}

	switch stringValue(details["reason"]) {
	case "max_output_tokens", finishReasonMaxTokens:
		return llm.FinishReasonLength
	case "content_filter":
		return llm.FinishReasonContentFilter
	default:
		return llm.FinishReasonError
	}
}

func openAIResponseOutputItemsFinishReason(toolCalls []ToolCall) llm.FinishReason {
	if len(toolCalls) > 0 {
		return llm.FinishReasonToolCalls
	}

	return llm.FinishReasonUnknown
}

func outputItemsFromResponse(output any) []any {
	items, ok := output.([]any)
	if !ok {
		return nil
	}

	cloned := make([]any, 0, len(items))
	cloned = append(cloned, items...)

	return cloned
}

func toolCallsFromOutput(output []any) []ToolCall {
	calls := []ToolCall{}

	for _, item := range output {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object[jsonTypeKey]) != functionCallType {
			continue
		}

		argumentsJSON := stringValue(object[jsonArgumentsKey])

		arguments := toolArgumentsFromJSON(argumentsJSON)

		calls = append(calls, ToolCall{
			Arguments:     arguments,
			Metadata:      nil,
			ID:            firstNonEmptyString(stringValue(object[jsonCallIDKey]), stringValue(object["id"])),
			Name:          firstNonEmptyString(stringValue(object[jsonToolNameKey]), stringValue(object["function"])),
			ArgumentsJSON: argumentsJSON,
		})
	}

	return calls
}

func thinkingFromOutput(output []any) []string {
	thinking := []string{}

	for _, item := range output {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object[jsonTypeKey]) != jsonReasoningKey {
			continue
		}

		text := strings.TrimSpace(extractThinkingText(object["summary"]))
		if text != "" {
			thinking = append(thinking, text)
		}
	}

	return thinking
}

func extractThinkingText(value any) string {
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := extractThinkingText(item); text != "" {
				parts = append(parts, text)
			}
		}

		return strings.Join(parts, "\n\n")
	case map[string]any:
		return stringValue(typed["text"])
	case string:
		return typed
	default:
		return ""
	}
}

func extractText(value any) string {
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := extractText(item); text != "" {
				parts = append(parts, text)
			}
		}

		return strings.Join(parts, "")
	case map[string]any:
		if text, ok := typed["text"].(string); ok {
			return text
		}

		if content, ok := typed[jsonContentKey]; ok {
			return extractText(content)
		}

		if output, ok := typed[jsonOutputKey]; ok {
			return extractText(output)
		}
	}

	return ""
}

func codexReasoning(request *CompletionRequest) map[string]string {
	if effort, ok := reasoningEffort(request); ok {
		return map[string]string{reasoningEffortKey: effort, jsonSummaryKey: reasoningSummaryAuto}
	}

	return map[string]string{reasoningEffortKey: reasoningEffortNone, jsonSummaryKey: reasoningSummaryAuto}
}
