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
	payload := anthropicPayload(request)
	endpoint := joinEndpoint(request.Model.BaseURL, "/v1/messages")
	content, err := client.postJSON(ctx, endpoint, anthropicHeaders(request), payload)
	if err != nil {
		return nil, err
	}
	var response struct {
		Error   providerError `json:"error"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, oops.In("assistant").Code("anthropic_decode").Wrapf(err, "decode anthropic response")
	}
	if response.Error.Message != "" {
		return nil, providerErrorToOops("anthropic_error", &response.Error)
	}
	parts := make([]string, 0, len(response.Content))
	for _, block := range response.Content {
		if block.Type == jsonTextKey && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return nil, oops.In("assistant").Code("anthropic_empty").Errorf("provider returned an empty response")
	}

	return textCompletionResult(text), nil
}

func anthropicPayload(request *CompletionRequest) map[string]any {
	// Anthropic's recent Claude models reject temperature when thinking/adaptive
	// reasoning is available. Match production agent clients by omitting
	// temperature unless/until librecode exposes an explicit user setting.
	payload := map[string]any{
		jsonModelKey: request.Model.ID,
		"max_tokens": minPositive(request.Model.MaxTokens, 4096),
		"messages":   anthropicMessages(request.Messages),
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
	features := []string{"fine-grained-tool-streaming-2025-05-14"}
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

func anthropicMessages(messages []database.MessageEntity) []map[string]string {
	output := []map[string]string{}
	for _, message := range messages {
		role, ok := anthropicRole(message.Role)
		if !ok || message.Content == "" {
			continue
		}
		output = append(output, map[string]string{jsonRoleKey: role, jsonContentKey: message.Content})
	}

	return output
}

func anthropicRole(role database.Role) (string, bool) {
	switch role {
	case database.RoleUser:
		return jsonUserRole, true
	case database.RoleAssistant:
		return "assistant", true
	case database.RoleToolResult,
		database.RoleThinking,
		database.RoleCustom,
		database.RoleBashExecution,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return "", false
	}

	return "", false
}
