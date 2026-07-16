package provider

import (
	"context"
	"io"

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
	payload := client.openAIChatPayload(request, state.messages)
	headers := openAIHeaders(request)

	providerRequest, err := applyProviderRequestHook(ctx, request, payload, headers)
	if err != nil {
		return false, err
	}

	providerResult, err := client.requestProviderStream(
		ctx,
		state.endpoint,
		providerRequest.Headers,
		providerRequest.Payload,
		func(reader io.Reader) (*providerResult, error) {
			return parseOpenAIChatStream(reader, request.OnEvent)
		},
	)
	if err != nil {
		return false, err
	}

	state.result.Usage = accumulateUsage(state.result.Usage, providerResult.Usage)
	if validateErr := validateToolCalls(providerResult.ToolCalls); validateErr != nil {
		return false, validateErr
	}

	if len(providerResult.ToolCalls) == 0 {
		return finishProviderResult(state.result, providerResult)
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

func openAIChatPayload(request *CompletionRequest) map[string]any {
	return buildOpenAIChatPayload(request, nil)
}

func (client *HTTPCompletionClient) openAIChatPayload(
	request *CompletionRequest,
	messages []map[string]any,
) map[string]any {
	return buildOpenAIChatPayload(request, messages)
}

func buildOpenAIChatPayload(request *CompletionRequest, messages []map[string]any) map[string]any {
	tools := openAIChatTools(requestToolDefinitions(request))

	payload := map[string]any{
		jsonModelKey:    request.Request.Model.ID,
		jsonMessagesKey: messages,
		jsonStreamKey:   true,
		"stream_options": map[string]any{
			"include_usage": true,
		},
		"temperature":     openAIChatDefaultTemperature,
		jsonToolsKey:      tools,
		jsonToolChoiceKey: "auto",
	}
	if effort, ok := reasoningEffort(request); ok {
		payload["reasoning_effort"] = effort
	}

	addZAIChatPayloadOptions(payload, request, len(tools) > 0)

	return payload
}

func openAIChatFinishReason(reason string, hasToolCalls bool) llm.FinishReason {
	if hasToolCalls {
		return llm.FinishReasonToolCalls
	}

	switch reason {
	case openAIStopReason:
		return llm.FinishReasonStop
	case "length":
		return llm.FinishReasonLength
	case openAIToolCallsReason, "function_call":
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
		jsonRoleKey:      jsonAssistantRole,
		jsonContentKey:   result.Text,
		jsonToolCallsKey: toolCalls,
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
	for index := range events {
		messages = append(messages, map[string]any{
			jsonRoleKey:    jsonToolRole,
			"tool_call_id": calls[index].ID,
			jsonContentKey: toolOutputText(events[index].Result, events[index].DetailsJSON),
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
