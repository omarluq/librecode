package provider

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

func (client *HTTPCompletionClient) completeOpenAIChat(
	ctx context.Context,
	request *CompletionRequest,
) (*llm.Response, error) {
	state := openAIChatLoopState{
		messages: openAIChatMessages(request),
		endpoint: joinEndpoint(request.Request.Model.BaseURL, "/chat/completions"),
		result:   newResponse(),
	}
	for {
		finished, err := client.advanceOpenAIChatLoop(ctx, request, &state)
		if err != nil {
			return nil, err
		}

		if finished {
			return state.result, nil
		}
	}
}

type openAIChatLoopState struct {
	result   *llm.Response
	endpoint string
	messages []map[string]any
}

func (client *HTTPCompletionClient) advanceOpenAIChatLoop(
	ctx context.Context,
	request *CompletionRequest,
	state *openAIChatLoopState,
) (bool, error) {
	payload := openAIChatPayload(request, state.messages)
	headers := openAIHeaders(request)

	providerRequest, err := applyProviderRequestHook(ctx, request, payload, headers)
	if err != nil {
		return false, err
	}

	content, err := client.postJSON(ctx, state.endpoint, providerRequest.Headers, providerRequest.Payload)
	if err != nil {
		return false, err
	}

	providerResult, err := parseOpenAIChatResult(content)
	if err != nil {
		return false, err
	}

	state.result.Usage = mergeUsage(state.result.Usage, providerResult.Usage)
	if validateErr := validateToolCalls(providerResult.ToolCalls); validateErr != nil {
		return false, validateErr
	}

	if len(providerResult.ToolCalls) == 0 {
		if fallback := TextToolCallsFromText(providerResult.Text); len(fallback) > 0 {
			providerResult.ToolCalls = fallback
		} else {
			return finishProviderResult(state.result, providerResult)
		}
	}

	events, err := executeOpenAIChatToolCalls(ctx, request, providerResult.ToolCalls)
	if err != nil {
		return false, err
	}

	appendToolResults(state.result, events)

	if err := appendOpenAIChatToolConversation(state, providerResult, events); err != nil {
		return false, err
	}

	return false, nil
}

func executeOpenAIChatToolCalls(
	ctx context.Context,
	request *CompletionRequest,
	calls []ToolCall,
) ([]ToolEvent, error) {
	_, events, err := executeToolCalls(ctx, request, calls)
	if err != nil {
		return nil, err
	}

	return events, nil
}

func appendOpenAIChatToolConversation(state *openAIChatLoopState, result *providerResult, events []ToolEvent) error {
	if HasTextFallbackToolCalls(result.ToolCalls) {
		state.messages = append(
			state.messages,
			map[string]any{jsonRoleKey: jsonAssistantRole, jsonContentKey: result.Text},
			map[string]any{jsonRoleKey: jsonUserRole, jsonContentKey: TextToolResultPrompt(events)},
		)

		return nil
	}

	toolMessages, err := openAIChatToolMessages(result.ToolCalls, events)
	if err != nil {
		return err
	}

	state.messages = append(
		state.messages,
		openAIChatAssistantToolMessage(result),
	)
	state.messages = append(state.messages, toolMessages...)

	return nil
}

const openAIChatDefaultTemperature = 0.2

func openAIChatPayload(request *CompletionRequest, messages []map[string]any) map[string]any {
	payload := map[string]any{
		jsonModelKey:      request.Request.Model.ID,
		jsonMessagesKey:   messages,
		"stream":          false,
		"temperature":     openAIChatDefaultTemperature,
		"tools":           OpenAIChatTools(request),
		jsonToolChoiceKey: "auto",
	}
	if effort, ok := reasoningEffort(request); ok {
		payload["reasoning_effort"] = effort
	}

	return payload
}

func parseOpenAIChatResult(content []byte) (*providerResult, error) {
	var response struct {
		Error   providerError  `json:"error"`
		Usage   map[string]any `json:"usage"`
		Choices []struct {
			FinishReason string `json:"finish_reason"`
			Message      struct {
				Content   string `json:"content"`
				ToolCalls []struct {
					ID       string `json:"id"`
					Type     string `json:"type"`
					Function struct {
						Name      string `json:"name"`
						Arguments string `json:"arguments"`
					} `json:"function"`
				} `json:"tool_calls"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, oops.In("provider").Code("openai_chat_decode").Wrapf(err, "decode chat response")
	}

	if response.Error.Message != "" {
		return nil, providerErrorToOops("openai_chat_error", &response.Error)
	}

	if len(response.Choices) == 0 {
		return &providerResult{
			FinishReason: llm.FinishReasonUnknown,
			Text:         "",
			OutputItems:  nil,
			Thinking:     nil,
			ToolCalls:    nil,
			Usage:        usageFromObject(response.Usage),
		}, nil
	}

	message := response.Choices[0].Message

	calls := make([]ToolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		if call.Type != "" && call.Type != functionToolType {
			continue
		}

		calls = append(calls, ToolCall{
			Arguments:     toolArgumentsFromJSON(call.Function.Arguments),
			Metadata:      nil,
			ID:            call.ID,
			Name:          call.Function.Name,
			ArgumentsJSON: call.Function.Arguments,
			TextFallback:  false,
		})
	}

	return &providerResult{
		FinishReason: openAIChatFinishReason(response.Choices[0].FinishReason, len(calls) > 0),
		Text:         strings.TrimSpace(message.Content),
		OutputItems:  nil,
		Thinking:     nil,
		ToolCalls:    calls,
		Usage:        usageFromObject(response.Usage),
	}, nil
}

func openAIChatFinishReason(reason string, hasToolCalls bool) llm.FinishReason {
	if hasToolCalls {
		return llm.FinishReasonToolCalls
	}

	switch reason {
	case "stop":
		return llm.FinishReasonStop
	case "length":
		return llm.FinishReasonLength
	case "tool_calls", "function_call":
		return llm.FinishReasonToolCalls
	case "content_filter":
		return llm.FinishReasonContentFilter
	default:
		return llm.FinishReasonUnknown
	}
}

func reasoningEffort(request *CompletionRequest) (string, bool) {
	if !request.Request.Model.Reasoning ||
		request.Request.ThinkingLevel == "" ||
		request.Request.ThinkingLevel == thinkingOff {
		return "", false
	}

	if mapped := request.Request.Model.ThinkingLevelMap[request.Request.ThinkingLevel]; mapped != nil {
		return *mapped, true
	}

	return request.Request.ThinkingLevel, true
}

func openAIChatMessages(request *CompletionRequest) []map[string]any {
	messages := []map[string]any{}
	if request.Request.SystemPrompt != "" {
		messages = append(messages, map[string]any{
			jsonRoleKey:    jsonSystemRole,
			jsonContentKey: request.Request.SystemPrompt,
		})
	}

	for _, message := range request.Request.Messages {
		role, ok := openAIRole(message.Role)

		content := messageText(message)
		if !ok || content == "" {
			continue
		}

		messages = append(messages, map[string]any{jsonRoleKey: role, jsonContentKey: content})
	}

	return messages
}

func openAIChatAssistantToolMessage(result *providerResult) map[string]any {
	toolCalls := make([]map[string]any, 0, len(result.ToolCalls))
	for _, call := range result.ToolCalls {
		toolCalls = append(toolCalls, map[string]any{
			"id":        call.ID,
			jsonTypeKey: functionToolType,
			jsonFunctionKey: map[string]any{
				jsonToolNameKey:  call.Name,
				jsonArgumentsKey: call.ArgumentsJSON,
			},
		})
	}

	return map[string]any{
		jsonRoleKey:    jsonAssistantRole,
		jsonContentKey: result.Text,
		"tool_calls":   toolCalls,
	}
}

func openAIChatToolMessages(calls []ToolCall, events []ToolEvent) ([]map[string]any, error) {
	if len(events) != len(calls) {
		return nil, oops.In("provider").
			Code("openai_chat_tool_message_mismatch").
			With("calls", len(calls)).
			With("events", len(events)).
			Errorf("build OpenAI chat tool messages: mismatched tool calls and results")
	}

	messages := make([]map[string]any, 0, len(events))
	for index, event := range events {
		messages = append(messages, map[string]any{
			jsonRoleKey:    jsonToolRole,
			"tool_call_id": calls[index].ID,
			jsonContentKey: toolOutputText(event.Result, event.DetailsJSON),
		})
	}

	return messages, nil
}

func openAIRole(role llm.Role) (string, bool) {
	switch role {
	case llm.RoleUser, llm.RoleSystem:
		return jsonUserRole, true
	case llm.RoleAssistant:
		return jsonAssistantRole, true
	case llm.RoleTool:
		return "", false
	}

	return "", false
}
