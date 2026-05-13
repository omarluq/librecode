package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/model"
)

func (client *HTTPCompletionClient) completeOpenAIResponses(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	input := openAIResponseInput(request.Messages)
	endpoint := joinEndpoint(request.Model.BaseURL, "/responses")

	return client.completeResponsesLoop(ctx, request, endpoint, openAIHeaders(request), input, false)
}

func (client *HTTPCompletionClient) completeOpenAICodex(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	input := openAIResponseInput(compactResponseMessages(request.Messages))
	endpoint := joinEndpoint(request.Model.BaseURL, "/codex/responses")

	return client.completeResponsesLoop(ctx, request, endpoint, codexHeaders(request), input, true)
}

func (client *HTTPCompletionClient) completeResponsesLoop(
	ctx context.Context,
	request *CompletionRequest,
	endpoint string,
	headers map[string]string,
	input []any,
	stream bool,
) (*CompletionResult, error) {
	result := &CompletionResult{Text: "", Thinking: nil, ToolEvents: nil, Usage: model.EmptyTokenUsage()}
	for {
		payload := responsesPayload(request, input, stream)
		providerResult, err := client.requestResponses(ctx, endpoint, headers, payload, stream, request.OnEvent)
		if err != nil {
			return nil, err
		}
		result.Thinking = append(result.Thinking, providerResult.Thinking...)
		result.Usage = mergeUsage(result.Usage, providerResult.Usage)
		if err := validateToolCalls(providerResult.ToolCalls); err != nil {
			return nil, err
		}
		if len(providerResult.ToolCalls) == 0 {
			if strings.TrimSpace(providerResult.Text) == "" {
				return nil, oops.In("assistant").Code("responses_empty").Errorf("provider returned an empty response")
			}
			result.Text = strings.TrimSpace(providerResult.Text)
			return result, nil
		}
		input = append(input, statelessResponseOutputItems(providerResult.OutputItems)...)
		outputs, events := executeToolCalls(ctx, request.CWD, providerResult.ToolCalls, request.OnEvent)
		result.ToolEvents = append(result.ToolEvents, events...)
		input = append(input, outputs...)
	}
}

func responsesPayload(request *CompletionRequest, input []any, stream bool) map[string]any {
	payload := responsesBasePayload(request, input, stream)
	payload["tools"] = responseTools()
	payload[jsonToolChoiceKey] = "auto"
	payload["parallel_tool_calls"] = true

	return payload
}

func responsesBasePayload(request *CompletionRequest, input []any, stream bool) map[string]any {
	payload := map[string]any{
		jsonModelKey: request.Model.ID,
		"store":      false,
		"stream":     stream,
		"input":      input,
	}
	if request.SystemPrompt != "" {
		payload["instructions"] = request.SystemPrompt
	}
	if stream {
		payload["text"] = map[string]string{"verbosity": "low"}
		payload["include"] = []string{"reasoning.encrypted_content"}
		payload["prompt_cache_key"] = request.SessionID
	}
	if request.Model.Reasoning && request.ThinkingLevel != "" && request.ThinkingLevel != thinkingOff {
		payload["reasoning"] = map[string]any{
			reasoningEffortKey: request.ThinkingLevel,
			jsonSummaryKey:     reasoningSummaryAuto,
		}
	} else if stream {
		payload["reasoning"] = codexReasoning(request)
	}

	return payload
}

func (client *HTTPCompletionClient) requestResponses(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
	stream bool,
	onEvent func(StreamEvent),
) (*providerResult, error) {
	httpRequest, err := jsonRequest(ctx, endpoint, headers, payload)
	if err != nil {
		return nil, err
	}
	response, err := client.client.Do(httpRequest)
	if err != nil {
		return nil, oops.In("assistant").Code("responses_http").Wrapf(err, "request provider response")
	}
	defer closeBody(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		content, readErr := readProviderBody(response.Body)
		if readErr != nil {
			return nil, oops.In("assistant").Code("responses_error_read").Wrapf(readErr, "read provider error")
		}

		return nil, providerStatusError("responses_status", response.StatusCode, content)
	}
	if stream {
		return parseSSEResult(response.Body, onEvent)
	}
	content, err := readProviderBody(response.Body)
	if err != nil {
		return nil, oops.In("assistant").Code("responses_read").Wrapf(err, "read provider response")
	}

	return parseOpenAIResponseResult(content)
}

func statelessResponseOutputItems(items []any) []any {
	stateless := make([]any, 0, len(items))
	for _, item := range items {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object[jsonTypeKey]) != functionCallType {
			continue
		}
		stateless = append(stateless, map[string]any{
			jsonTypeKey:     functionCallType,
			jsonCallIDKey:   stringValue(object[jsonCallIDKey]),
			jsonToolNameKey: stringValue(object[jsonToolNameKey]),
			"arguments":     stringValue(object["arguments"]),
			"status":        "completed",
		})
	}

	return stateless
}

func parseOpenAIResponseResult(content []byte) (*providerResult, error) {
	var response map[string]any
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, oops.In("assistant").Code("openai_response_decode").Wrapf(err, "decode response")
	}
	if errorValue, ok := response["error"]; ok {
		message := errorMessage(errorValue)
		if message != "" {
			return nil, oops.In("assistant").Code("openai_response_error").Errorf("%s", message)
		}
	}

	return providerResultFromResponse(response), nil
}

func providerResultFromResponse(response map[string]any) *providerResult {
	outputItems := outputItemsFromResponse(response[jsonOutputKey])
	text := strings.TrimSpace(extractText(response[jsonOutputKey]))
	if text == "" {
		if outputText, ok := response["output_text"].(string); ok {
			text = strings.TrimSpace(outputText)
		}
	}

	return &providerResult{
		Text:        text,
		OutputItems: outputItems,
		Thinking:    thinkingFromOutput(outputItems),
		ToolCalls:   toolCallsFromOutput(outputItems),
		Usage:       usageFromObject(response["usage"]),
	}
}

func providerResultFromOutputItems(outputItems []any, fallbackText string) *providerResult {
	text := strings.TrimSpace(extractText(outputItems))
	if text == "" {
		text = fallbackText
	}

	return &providerResult{
		Text:        text,
		OutputItems: outputItems,
		Thinking:    thinkingFromOutput(outputItems),
		ToolCalls:   toolCallsFromOutput(outputItems),
		Usage:       model.EmptyTokenUsage(),
	}
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

func toolCallsFromOutput(output []any) []toolCall {
	calls := []toolCall{}
	for _, item := range output {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object[jsonTypeKey]) != functionCallType {
			continue
		}
		argumentsJSON := stringValue(object["arguments"])
		arguments := map[string]any{}
		if strings.TrimSpace(argumentsJSON) != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
				arguments = map[string]any{}
			}
		}
		calls = append(calls, toolCall{
			Arguments:     arguments,
			ID:            stringValue(object[jsonCallIDKey]),
			Name:          stringValue(object[jsonToolNameKey]),
			ArgumentsJSON: argumentsJSON,
		})
	}

	return calls
}

func thinkingFromOutput(output []any) []string {
	thinking := []string{}
	for _, item := range output {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object[jsonTypeKey]) != "reasoning" {
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
		if content, ok := typed["content"]; ok {
			return extractText(content)
		}
		if output, ok := typed[jsonOutputKey]; ok {
			return extractText(output)
		}
	}

	return ""
}

func codexReasoning(request *CompletionRequest) map[string]string {
	if request.ThinkingLevel == "" || request.ThinkingLevel == thinkingOff {
		return map[string]string{reasoningEffortKey: "none", jsonSummaryKey: reasoningSummaryAuto}
	}

	return map[string]string{reasoningEffortKey: request.ThinkingLevel, jsonSummaryKey: reasoningSummaryAuto}
}
