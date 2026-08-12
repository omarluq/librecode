package provider

import (
	"context"
	"io"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
)

func (client *HTTPCompletionClient) completeOpenAIChat(
	ctx context.Context,
	request *CompletionRequest,
) (*llm.Response, error) {
	messages, err := openAIChatMessages(request)
	if err != nil {
		return nil, err
	}

	state := openAIChatLoopState{
		messages: messages,
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

	observeProviderResponse(ctx, request, providerResult.Usage)

	state.result.Usage = accumulateUsage(state.result.Usage, providerResult.Usage)
	if validateErr := validateToolDispatch(providerResult.FinishReason, providerResult.ToolCalls); validateErr != nil {
		return false, validateErr
	}

	if len(providerResult.ToolCalls) == 0 {
		steering, checkpointErr := runRoundCheckpoint(
			ctx,
			request,
			new(completedRound(providerResult, nil)),
		)
		if checkpointErr != nil {
			return false, checkpointErr
		}

		if len(steering) == 0 {
			return finishProviderResult(state.result, providerResult)
		}

		appendOpenAIChatSteeringConversation(state, providerResult, steering)

		return false, nil
	}

	_, events, err := executeToolCalls(ctx, request, providerResult.ToolCalls)
	if err != nil {
		return false, err
	}

	appendToolResults(state.result, events)

	appendErr := appendOpenAIChatToolConversation(state, providerResult, events)
	if appendErr != nil {
		return false, appendErr
	}

	steering, err := runRoundCheckpoint(
		ctx,
		request,
		new(completedRoundWithToolEvents(providerResult, events)),
	)
	if err != nil {
		return false, err
	}

	appendOpenAIChatUserMessages(state, steering)

	return false, nil
}

func appendOpenAIChatSteeringConversation(
	state *openAIChatLoopState,
	result *providerResult,
	steering []llm.Message,
) {
	if strings.TrimSpace(result.Text) != "" {
		appendOpenAIChatAssistantMessage(state, result)
	}

	appendOpenAIChatUserMessages(state, steering)
}

func appendOpenAIChatAssistantMessage(state *openAIChatLoopState, result *providerResult) {
	state.messages = append(state.messages, map[string]any{
		jsonRoleKey:    jsonAssistantRole,
		jsonContentKey: result.Text,
	})
}

func appendOpenAIChatUserMessages(state *openAIChatLoopState, messages []llm.Message) {
	for index := range messages {
		state.messages = append(state.messages, map[string]any{
			jsonRoleKey:    jsonUserRole,
			jsonContentKey: openAIChatUserContent(messages[index]),
		})
	}
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
	if reason == "length" {
		return llm.FinishReasonLength
	}

	if hasToolCalls {
		return llm.FinishReasonToolCalls
	}

	switch reason {
	case openAIStopReason:
		return llm.FinishReasonStop
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

func openAIChatMessages(request *CompletionRequest) ([]map[string]any, error) {
	if err := validateImageMessages(request.Request.Messages); err != nil {
		return nil, err
	}

	messages := []map[string]any{}
	if request.Request.SystemPrompt != "" {
		messages = append(messages, map[string]any{
			jsonRoleKey:    jsonSystemRole,
			jsonContentKey: request.Request.SystemPrompt,
		})
	}

	for _, message := range request.Request.Messages {
		role, ok := openAIRole(message.Role)

		if !ok {
			continue
		}

		var content any = messageText(message)
		if message.Role == llm.RoleUser {
			content = openAIChatUserContent(message)
		}

		if emptyMessageContent(content) {
			continue
		}

		messages = append(messages, map[string]any{jsonRoleKey: role, jsonContentKey: content})
	}

	return messages, nil
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
