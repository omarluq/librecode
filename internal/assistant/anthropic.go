package assistant

import (
	"context"
	"encoding/json"
	"maps"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func (client *HTTPCompletionClient) completeAnthropic(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	state := anthropicLoopState{
		messages: anthropicMessages(request.Messages),
		endpoint: joinEndpoint(request.Model.BaseURL, "/v1/messages"),
		result:   &CompletionResult{Text: "", Thinking: nil, ToolEvents: nil, Usage: model.EmptyTokenUsage()},
	}
	for {
		finished, err := client.advanceAnthropicLoop(ctx, request, &state)
		if err != nil {
			return nil, err
		}
		if finished {
			return state.result, nil
		}
	}
}

type anthropicLoopState struct {
	result   *CompletionResult
	endpoint string
	messages []map[string]any
}

func (client *HTTPCompletionClient) advanceAnthropicLoop(
	ctx context.Context,
	request *CompletionRequest,
	state *anthropicLoopState,
) (bool, error) {
	payload := anthropicPayload(request, state.messages)
	providerResult, err := client.requestAnthropic(ctx, state.endpoint, request, payload)
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
	events := executeAnthropicToolCalls(ctx, request, providerResult.ToolCalls)
	state.result.ToolEvents = append(state.result.ToolEvents, events...)
	if err := appendAnthropicToolConversation(request, state, providerResult, events); err != nil {
		return false, err
	}

	return false, nil
}

func executeAnthropicToolCalls(
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

func appendAnthropicToolConversation(
	request *CompletionRequest,
	state *anthropicLoopState,
	providerResult *providerResult,
	events []ToolEvent,
) error {
	if hasTextFallbackToolCalls(providerResult.ToolCalls) {
		state.messages = append(
			state.messages,
			map[string]any{jsonRoleKey: jsonAssistantRole, jsonContentKey: providerResult.Text},
			map[string]any{jsonRoleKey: jsonUserRole, jsonContentKey: textToolResultPrompt(events)},
		)
		return nil
	}
	toolResultMessage, err := anthropicToolResultMessage(providerResult.ToolCalls, events)
	if err != nil {
		return err
	}
	state.messages = append(
		state.messages,
		anthropicAssistantToolMessage(request, providerResult.ToolCalls),
		toolResultMessage,
	)

	return nil
}

func anthropicPayload(request *CompletionRequest, messages []map[string]any) map[string]any {
	// Anthropic's recent Claude models reject temperature when thinking/adaptive
	// reasoning is available. Match production agent clients by omitting
	// temperature unless/until librecode exposes an explicit user setting.
	payload := map[string]any{
		jsonModelKey:    request.Model.ID,
		"max_tokens":    minPositive(request.Model.MaxTokens, 4096),
		jsonMessagesKey: messages,
		"tools":         anthropicTools(request),
	}
	if usesAnthropicOAuth(request) {
		payload["system"] = anthropicOAuthSystemPrompt(request.SystemPrompt)
	} else if request.SystemPrompt != "" {
		payload["system"] = []map[string]any{anthropicSystemText(request.SystemPrompt)}
	}
	if request.Model.Reasoning {
		maps.Copy(payload, anthropicThinkingConfig(request))
	}

	return payload
}

func anthropicHeaders(request *CompletionRequest) map[string]string {
	headers := cloneHeaders(request.Auth.Headers)
	betaFeatures := anthropicBetaFeatures(request)
	if usesAnthropicOAuth(request) {
		headers["Authorization"] = "Bearer " + request.Auth.APIKey
		headers["anthropic-beta"] = appendAnthropicBeta(
			headers["anthropic-beta"],
			append([]string{"claude-code-20250219", "oauth-2025-04-20"}, betaFeatures...)...,
		)
		headers["user-agent"] = "claude-cli/2.1.2 (external, cli)"
		headers["x-app"] = "cli"
	} else {
		headers["x-api-key"] = request.Auth.APIKey
		headers["anthropic-beta"] = appendAnthropicBeta(headers["anthropic-beta"], betaFeatures...)
	}
	headers["anthropic-version"] = "2023-06-01"

	return headers
}

func usesAnthropicOAuth(request *CompletionRequest) bool {
	return request.Model.Provider == "anthropic-claude" || isAnthropicOAuthToken(request.Auth.APIKey)
}

func isAnthropicOAuthToken(apiKey string) bool {
	return strings.HasPrefix(apiKey, "sk-ant-oat")
}

func anthropicOAuthSystemPrompt(systemPrompt string) []map[string]any {
	blocks := []map[string]any{anthropicSystemText("You are Claude Code, Anthropic's official CLI for Claude.")}
	if systemPrompt != "" {
		blocks = append(blocks, anthropicSystemText(systemPrompt))
	}

	return blocks
}

func anthropicSystemText(text string) map[string]any {
	return map[string]any{
		jsonTypeKey: jsonTextKey,
		jsonTextKey: text,
		"cache_control": map[string]string{
			jsonTypeKey: "ephemeral",
		},
	}
}

func anthropicThinkingConfig(request *CompletionRequest) map[string]any {
	if request.ThinkingLevel == "" || request.ThinkingLevel == thinkingOff {
		return map[string]any{jsonThinkingKey: map[string]any{jsonTypeKey: "disabled"}}
	}
	if anthropicSupportsAdaptiveThinking(request.Model.ID) {
		config := map[string]any{
			jsonThinkingKey: map[string]any{jsonTypeKey: "adaptive", jsonDisplayKey: thinkingDisplaySummary},
		}
		if effort := anthropicThinkingEffort(request); effort != "" {
			config["output_config"] = map[string]any{"effort": effort}
		}

		return config
	}

	return map[string]any{jsonThinkingKey: anthropicBudgetThinking(request.ThinkingLevel)}
}

func anthropicBudgetThinking(level string) map[string]any {
	return map[string]any{
		jsonTypeKey:     "enabled",
		"budget_tokens": anthropicThinkingBudget(level),
		jsonDisplayKey:  thinkingDisplaySummary,
	}
}

func anthropicThinkingEffort(request *CompletionRequest) string {
	level := model.ThinkingLevel(request.ThinkingLevel)
	if mapped := request.Model.ThinkingLevelMap[level]; mapped != nil {
		return *mapped
	}
	switch request.ThinkingLevel {
	case "minimal", thinkingLow:
		return thinkingLow
	case "medium":
		return "medium"
	case thinkingHigh:
		return thinkingHigh
	case thinkingXHigh:
		return thinkingXHigh
	default:
		return thinkingHigh
	}
}

func anthropicSupportsAdaptiveThinking(modelID string) bool {
	normalizedModelID := strings.ToLower(modelID)

	return strings.Contains(normalizedModelID, "opus-4-6") ||
		strings.Contains(normalizedModelID, "opus-4.6") ||
		strings.Contains(normalizedModelID, "opus-4-7") ||
		strings.Contains(normalizedModelID, "opus-4.7") ||
		strings.Contains(normalizedModelID, "sonnet-4-6") ||
		strings.Contains(normalizedModelID, "sonnet-4.6")
}

func anthropicBetaFeatures(request *CompletionRequest) []string {
	features := []string{}
	if request.Model.Reasoning && !anthropicSupportsAdaptiveThinking(request.Model.ID) {
		features = append(features, "interleaved-thinking-2025-05-14")
	}

	return features
}

func anthropicThinkingBudget(level string) int {
	switch level {
	case "minimal":
		return 1024
	case thinkingLow:
		return 4096
	case thinkingHigh, thinkingXHigh:
		return 20480
	default:
		return 10240
	}
}

func appendAnthropicBeta(existing string, values ...string) string {
	seen := map[string]bool{}
	output := make([]string, 0, len(values)+1)
	items := append(strings.Split(existing, ","), values...)
	for _, item := range items {
		trimmed := strings.TrimSpace(item)
		if trimmed == "" || seen[trimmed] {
			continue
		}
		seen[trimmed] = true
		output = append(output, trimmed)
	}

	return strings.Join(output, ",")
}

func (client *HTTPCompletionClient) requestAnthropic(
	ctx context.Context,
	endpoint string,
	request *CompletionRequest,
	payload map[string]any,
) (*providerResult, error) {
	headers := anthropicHeaders(request)
	providerRequest, err := applyProviderRequestHook(ctx, request, payload, headers)
	if err != nil {
		return nil, err
	}
	content, err := client.postJSON(ctx, endpoint, providerRequest.Headers, providerRequest.Payload)
	if err != nil {
		return nil, err
	}

	return parseAnthropicResult(content)
}

func parseAnthropicResult(content []byte) (*providerResult, error) {
	var response struct {
		Error   providerError  `json:"error"`
		Usage   map[string]any `json:"usage"`
		Content []struct {
			Type  string `json:"type"`
			Text  string `json:"text"`
			Input any    `json:"input"`
			ID    string `json:"id"`
			Name  string `json:"name"`
		} `json:"content"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, oops.In("assistant").Code("anthropic_decode").Wrapf(err, "decode anthropic response")
	}
	if response.Error.Message != "" {
		return nil, providerErrorToOops("anthropic_error", &response.Error)
	}
	parts := make([]string, 0, len(response.Content))
	calls := make([]toolCall, 0, len(response.Content))
	for _, block := range response.Content {
		switch block.Type {
		case jsonTextKey:
			if block.Text != "" {
				parts = append(parts, block.Text)
			}
		case anthropicToolUseType:
			calls = append(calls, anthropicToolCall(block.ID, block.Name, block.Input))
		}
	}

	return &providerResult{
		Text:        strings.TrimSpace(strings.Join(parts, "\n")),
		OutputItems: nil,
		Thinking:    nil,
		ToolCalls:   calls,
		Usage:       usageFromObject(response.Usage),
	}, nil
}

func anthropicToolCall(id, name string, input any) toolCall {
	arguments, argumentsJSON := anthropicToolArguments(input)

	return toolCall{
		Arguments:     arguments,
		ID:            id,
		Name:          anthropicLocalToolName(name),
		ArgumentsJSON: argumentsJSON,
		TextFallback:  false,
	}
}

func anthropicToolArguments(input any) (arguments map[string]any, argumentsJSON string) {
	arguments = map[string]any{}
	payload, err := json.Marshal(input)
	if err != nil {
		return arguments, "{}"
	}
	if len(payload) == 0 || string(payload) == "null" {
		return arguments, "{}"
	}
	if err := json.Unmarshal(payload, &arguments); err != nil {
		return map[string]any{}, string(payload)
	}

	return arguments, string(payload)
}

func anthropicAssistantToolMessage(request *CompletionRequest, calls []toolCall) map[string]any {
	blocks := make([]map[string]any, 0, len(calls))
	for _, call := range calls {
		blocks = append(blocks, map[string]any{
			jsonTypeKey:     anthropicToolUseType,
			"id":            call.ID,
			jsonToolNameKey: anthropicProviderToolName(call.Name, usesAnthropicOAuth(request)),
			"input":         call.Arguments,
		})
	}

	return map[string]any{jsonRoleKey: jsonAssistantRole, jsonContentKey: blocks}
}

func anthropicToolResultMessage(calls []toolCall, events []ToolEvent) (map[string]any, error) {
	if len(events) != len(calls) {
		return nil, oops.In("assistant").
			Code("anthropic_tool_message_mismatch").
			With("calls", len(calls)).
			With("events", len(events)).
			Errorf("build Anthropic tool result message: mismatched tool calls and results")
	}
	blocks := make([]map[string]any, 0, len(events))
	for index, event := range events {
		block := map[string]any{
			jsonTypeKey:    anthropicToolResultType,
			"tool_use_id":  calls[index].ID,
			jsonContentKey: toolOutputText(event.Result, event.DetailsJSON),
		}
		if event.IsError {
			block["is_error"] = true
		}
		blocks = append(blocks, block)
	}

	return map[string]any{jsonRoleKey: jsonUserRole, jsonContentKey: blocks}, nil
}

func anthropicMessages(messages []database.MessageEntity) []map[string]any {
	output := []map[string]any{}
	for _, message := range messages {
		role, ok := anthropicRole(message.Role)
		if !ok || message.Content == "" {
			continue
		}
		output = append(output, map[string]any{jsonRoleKey: role, jsonContentKey: message.Content})
	}

	return output
}

func anthropicTools(request *CompletionRequest) []map[string]any {
	definitions := requestToolDefinitions(request)
	tools := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, map[string]any{
			jsonToolNameKey:         anthropicProviderToolName(string(definition.Name), usesAnthropicOAuth(request)),
			jsonDescriptionKey:      definition.Description,
			jsonInputSchemaKey:      toolParameterSchema(&definition),
			"eager_input_streaming": true,
		})
	}

	return tools
}

func anthropicProviderToolName(name string, oauth bool) string {
	if !oauth {
		return name
	}
	switch name {
	case jsonReadToolName:
		return anthropicReadToolName
	case jsonWriteToolName:
		return anthropicWriteToolName
	case jsonEditToolName:
		return anthropicEditToolName
	case jsonBashToolName:
		return anthropicBashToolName
	case jsonGrepToolName:
		return anthropicGrepToolName
	case jsonFindToolName:
		return anthropicFindToolName
	case jsonLSToolName:
		return anthropicLSToolName
	default:
		return name
	}
}

func anthropicLocalToolName(name string) string {
	switch strings.TrimSpace(name) {
	case anthropicReadToolName:
		return jsonReadToolName
	case anthropicWriteToolName:
		return jsonWriteToolName
	case anthropicEditToolName:
		return jsonEditToolName
	case anthropicBashToolName:
		return jsonBashToolName
	case anthropicGrepToolName:
		return jsonGrepToolName
	case anthropicFindToolName:
		return jsonFindToolName
	case anthropicLSToolName, "List":
		return jsonLSToolName
	default:
		return normalizeTextToolName(name)
	}
}

func anthropicRole(role database.Role) (string, bool) {
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
