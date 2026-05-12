package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
)

func TestAnthropicPayloadOmitsTemperature(t *testing.T) {
	t.Parallel()

	payload := anthropicPayload(testCompletionRequestAuth("anthropic-claude", "subscription-access-token"))

	assert.NotContains(t, payload, "temperature")
	assert.Equal(t, "", payload[jsonModelKey])
	assert.Equal(t, 4096, payload["max_tokens"])
}

const anthropicTestSystemPrompt = "system"

func TestAnthropicPayloadUsesStructuredSystemPrompt(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	request.SystemPrompt = anthropicTestSystemPrompt
	payload := anthropicPayload(request)

	assert.Equal(t, []map[string]any{anthropicSystemText(anthropicTestSystemPrompt)}, payload["system"])
}

func TestAnthropicOAuthPayloadAddsClaudeCodeIdentity(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("anthropic-claude", "sk-ant-oat-secret")
	request.SystemPrompt = anthropicTestSystemPrompt
	payload := anthropicPayload(request)

	systemBlocks, ok := payload["system"].([]map[string]any)
	assert.True(t, ok)
	assert.Len(t, systemBlocks, 2)
	assert.Contains(t, systemBlocks[0]["text"], "Claude Code")
	assert.Equal(t, anthropicTestSystemPrompt, systemBlocks[1][jsonTextKey])
}

func TestAnthropicPayloadAddsBudgetThinking(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	request.Model.ID = "claude-sonnet-4-5"
	request.Model.Reasoning = true
	request.ThinkingLevel = thinkingLow
	payload := anthropicPayload(request)

	assert.Equal(t, map[string]any{
		jsonTypeKey:     "enabled",
		"budget_tokens": 4096,
		jsonDisplayKey:  thinkingDisplaySummary,
	}, payload[jsonThinkingKey])
}

func TestAnthropicPayloadDisablesThinkingWhenOff(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	request.Model.ID = "claude-opus-4-7"
	request.Model.Reasoning = true
	request.ThinkingLevel = thinkingOff
	payload := anthropicPayload(request)

	assert.Equal(t, map[string]any{jsonTypeKey: "disabled"}, payload[jsonThinkingKey])
	assert.NotContains(t, payload, "output_config")
}

func TestAnthropicPayloadAddsAdaptiveThinking(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("sk-ant-api03-secret")
	request.Model.ID = "claude-opus-4-7"
	request.Model.Reasoning = true
	request.ThinkingLevel = thinkingXHigh
	payload := anthropicPayload(request)

	assert.Equal(t, map[string]any{
		jsonTypeKey:    "adaptive",
		jsonDisplayKey: thinkingDisplaySummary,
	}, payload[jsonThinkingKey])
	assert.Equal(t, map[string]any{reasoningEffortKey: thinkingXHigh}, payload["output_config"])
}

func TestAnthropicHeadersUseAPIKeyByDefault(t *testing.T) {
	t.Parallel()

	headers := anthropicHeaders(testCompletionRequestAuth("sk-ant-api03-secret"))

	assert.Equal(t, "sk-ant-api03-secret", headers["x-api-key"])
	assert.Empty(t, headers["Authorization"])
	assert.Equal(t, "2023-06-01", headers["anthropic-version"])
	assert.NotContains(t, headers["anthropic-beta"], "interleaved-thinking-2025-05-14")
}

func TestAnthropicHeadersUseBearerForOAuth(t *testing.T) {
	t.Parallel()

	headers := anthropicHeaders(testCompletionRequestAuth("anthropic-claude", "subscription-access-token"))

	assert.Empty(t, headers["x-api-key"])
	assert.Equal(t, "Bearer subscription-access-token", headers["Authorization"])
	assert.Equal(t, "2023-06-01", headers["anthropic-version"])
	assert.Equal(t, "cli", headers["x-app"])
	assert.Equal(t, "claude-cli/2.1.2 (external, cli)", headers["user-agent"])
	assert.Contains(t, headers["anthropic-beta"], "claude-code-20250219")
	assert.Contains(t, headers["anthropic-beta"], "oauth-2025-04-20")
	assert.NotContains(t, headers["anthropic-beta"], "interleaved-thinking-2025-05-14")
}

func testCompletionRequestAuth(args ...string) *CompletionRequest {
	provider := "anthropic"
	apiKey := ""
	if len(args) == 1 {
		apiKey = args[0]
	}
	if len(args) > 1 {
		provider = args[0]
		apiKey = args[1]
	}

	return &CompletionRequest{
		OnEvent:       nil,
		SessionID:     "",
		SystemPrompt:  "",
		ThinkingLevel: "",
		CWD:           "",
		Auth:          testRequestAuth(apiKey),
		Messages:      nil,
		Model: model.Model{
			ThinkingLevelMap: nil,
			Headers:          nil,
			Compat:           nil,
			Provider:         provider,
			ID:               "",
			Name:             "",
			API:              "",
			BaseURL:          "",
			Input:            nil,
			Cost:             model.Cost{Input: 0, Output: 0, CacheRead: 0, CacheWrite: 0},
			ContextWindow:    0,
			MaxTokens:        0,
			Reasoning:        false,
		},
	}
}

func testRequestAuth(apiKey string) model.RequestAuth {
	return model.RequestAuth{Headers: nil, APIKey: apiKey, Error: "", OK: true}
}
