package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func (client *HTTPCompletionClient) completeOpenAIChat(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	state := openAIChatLoopState{
		messages: openAIChatMessages(request),
		endpoint: joinEndpoint(request.Model.BaseURL, "/chat/completions"),
		result:   &CompletionResult{Text: "", Thinking: nil, ToolEvents: nil, Usage: model.EmptyTokenUsage()},
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
	result   *CompletionResult
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
	if err := validateToolCalls(providerResult.ToolCalls); err != nil {
		return false, err
	}
	if len(providerResult.ToolCalls) == 0 {
		if fallback := textToolCallsFromText(providerResult.Text); len(fallback) > 0 {
			providerResult.ToolCalls = fallback
		} else {
			return finishTextResult(state.result, providerResult.Text)
		}
	}
	events := executeOpenAIChatToolCalls(ctx, request, providerResult.ToolCalls)
	state.result.ToolEvents = append(state.result.ToolEvents, events...)
	if err := appendOpenAIChatToolConversation(state, providerResult, events); err != nil {
		return false, err
	}

	return false, nil
}

func executeOpenAIChatToolCalls(
	ctx context.Context,
	request *CompletionRequest,
	calls []toolCall,
) []ToolEvent {
	_, events := executeToolCalls(
		ctx,
		request.ToolRegistry,
		request.CWD,
		calls,
		request.OnEvent,
		request.OnToolCall,
		request.OnToolResult,
	)

	return events
}

func appendOpenAIChatToolConversation(state *openAIChatLoopState, result *providerResult, events []ToolEvent) error {
	if hasTextFallbackToolCalls(result.ToolCalls) {
		state.messages = append(
			state.messages,
			map[string]any{jsonRoleKey: jsonAssistantRole, jsonContentKey: result.Text},
			map[string]any{jsonRoleKey: jsonUserRole, jsonContentKey: textToolResultPrompt(events)},
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

func openAIChatPayload(request *CompletionRequest, messages []map[string]any) map[string]any {
	payload := map[string]any{
		jsonModelKey:      request.Model.ID,
		jsonMessagesKey:   messages,
		"stream":          false,
		"temperature":     0.2,
		"tools":           openAIChatTools(request),
		jsonToolChoiceKey: "auto",
	}
	if request.Model.Reasoning && request.ThinkingLevel != "" && request.ThinkingLevel != thinkingOff {
		payload["reasoning_effort"] = request.ThinkingLevel
	}

	return payload
}

func parseOpenAIChatResult(content []byte) (*providerResult, error) {
	var response struct {
		Error   providerError  `json:"error"`
		Usage   map[string]any `json:"usage"`
		Choices []struct {
			Message struct {
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
		return nil, oops.In("assistant").Code("openai_chat_decode").Wrapf(err, "decode chat response")
	}
	if response.Error.Message != "" {
		return nil, providerErrorToOops("openai_chat_error", &response.Error)
	}
	if len(response.Choices) == 0 {
		return &providerResult{
			Text:        "",
			OutputItems: nil,
			Thinking:    nil,
			ToolCalls:   nil,
			Usage:       usageFromObject(response.Usage),
		}, nil
	}
	message := response.Choices[0].Message
	calls := make([]toolCall, 0, len(message.ToolCalls))
	for _, call := range message.ToolCalls {
		if call.Type != "" && call.Type != functionToolType {
			continue
		}
		calls = append(calls, toolCall{
			Arguments:     toolArgumentsFromJSON(call.Function.Arguments),
			ID:            call.ID,
			Name:          call.Function.Name,
			ArgumentsJSON: call.Function.Arguments,
			TextFallback:  false,
		})
	}

	return &providerResult{
		Text:        strings.TrimSpace(message.Content),
		OutputItems: nil,
		Thinking:    nil,
		ToolCalls:   calls,
		Usage:       usageFromObject(response.Usage),
	}, nil
}

func openAIChatMessages(request *CompletionRequest) []map[string]any {
	messages := []map[string]any{}
	if request.SystemPrompt != "" {
		messages = append(messages, map[string]any{jsonRoleKey: jsonSystemRole, jsonContentKey: request.SystemPrompt})
	}
	for _, message := range request.Messages {
		role, ok := openAIRole(message.Role)
		if !ok || message.Content == "" {
			continue
		}
		messages = append(messages, map[string]any{jsonRoleKey: role, jsonContentKey: message.Content})
	}

	return messages
}

func openAIChatAssistantToolMessage(result *providerResult) map[string]any {
	toolCalls := make([]map[string]any, 0, len(result.ToolCalls))
	for _, call := range result.ToolCalls {
		toolCalls = append(toolCalls, map[string]any{
			"id":        call.ID,
			jsonTypeKey: functionToolType,
			"function": map[string]any{
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

func openAIChatToolMessages(calls []toolCall, events []ToolEvent) ([]map[string]any, error) {
	if len(events) != len(calls) {
		return nil, oops.In("assistant").
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

func openAIRole(role database.Role) (string, bool) {
	switch role {
	case database.RoleUser:
		return jsonUserRole, true
	case database.RoleAssistant:
		return jsonAssistantRole, true
	case database.RoleBranchSummary, database.RoleCompactionSummary:
		return jsonUserRole, true
	case database.RoleCustom, database.RoleBashExecution:
		return jsonUserRole, true
	case database.RoleToolResult,
		database.RoleThinking:
		return "", false
	}

	return "", false
}
