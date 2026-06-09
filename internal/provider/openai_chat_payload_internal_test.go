package provider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func TestOpenAIChatPayloadAndRoles(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth(testOpenAIProvider, "sk-test")
	request.Model.ID = "gpt-test"
	request.Model.Reasoning = true
	request.ThinkingLevel = thinkingHigh
	payload := openAIChatPayload(request, nil)

	assert.Equal(t, "gpt-test", payload[jsonModelKey])
	assert.Equal(t, thinkingHigh, payload["reasoning_effort"])
	assert.Equal(t, "auto", payload[jsonToolChoiceKey])
	assert.NotEmpty(t, payload["tools"])
}

func TestOpenAIChatMessagesAndRoles(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth(testOpenAIProvider, "sk-test")
	request.SystemPrompt = "system"
	request.Messages = []database.MessageEntity{
		providerTestMessageEntity(database.RoleUser, jsonUserRole),
		providerTestMessageEntity(database.RoleAssistant, jsonAssistantRole),
		providerTestMessageEntity(database.RoleBranchSummary, testBranchContent),
		providerTestMessageEntity(database.RoleCompactionSummary, testCompactionContent),
		providerTestMessageEntity(database.RoleCustom, testCustomContent),
		providerTestMessageEntity(database.RoleBashExecution, jsonBashToolName),
		providerTestMessageEntity(database.RoleToolResult, jsonToolRole),
		providerTestMessageEntity(database.RoleThinking, testThinkingDelta),
		providerTestMessageEntity(database.RoleUser, ""),
	}

	messages := openAIChatMessages(request)

	assert.Len(t, messages, 7)
	assert.Equal(t, jsonSystemRole, messages[0][jsonRoleKey])
	assert.Equal(t, jsonUserRole, messages[1][jsonRoleKey])
	assert.Equal(t, jsonAssistantRole, messages[2][jsonRoleKey])
	for _, role := range []database.Role{database.RoleToolResult, database.RoleThinking} {
		mapped, ok := openAIRole(role)
		assert.False(t, ok)
		assert.Empty(t, mapped)
	}
}

func TestParseOpenAIChatResultHandlesErrorsAndToolFiltering(t *testing.T) {
	t.Parallel()

	_, err := parseOpenAIChatResult([]byte(`{"error":{"message":"bad chat"}}`))
	require.Error(t, err)
	assert.Contains(t, err.Error(), "bad chat")

	body := strings.ReplaceAll(`{
		"choices":[{"message":{"content":" answer ","tool_calls":[
			{"id":"call_skip","type":"not_function","function":{"name":"read","arguments":"{}"}},
			{"id":"$CALL_ID","type":"function","function":{"name":"read","arguments":"{\"path\":\"$PATH\"}"}}
		]}}],
		"usage":{"prompt_tokens":8,"completion_tokens":2}
	}`, "$CALL_ID", testCallID)
	body = strings.ReplaceAll(body, "$PATH", testToolPath)
	result, err := parseOpenAIChatResult([]byte(body))

	require.NoError(t, err)
	assert.Equal(t, "answer", result.Text)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, testCallID, result.ToolCalls[0].ID)
	assert.Equal(t, testToolPath, result.ToolCalls[0].Arguments[jsonPathKey])
	assert.Equal(t, model.TokenUsage{
		Breakdown: nil, TopContributors: nil, ContextWindow: 0, ContextTokens: 0,
		InputTokens: 8, OutputTokens: 2,
	}, result.Usage)
}

func TestOpenAIChatAssistantToolMessage(t *testing.T) {
	t.Parallel()

	message := openAIChatAssistantToolMessage(&providerResult{
		Text:        "using tool",
		OutputItems: nil,
		Thinking:    nil,
		ToolCalls: []ToolCall{{
			Arguments:     nil,
			ID:            testCallID,
			Name:          jsonReadToolName,
			ArgumentsJSON: testToolArgumentsJSON,
			TextFallback:  false,
		}},
		Usage: model.EmptyTokenUsage(),
	})

	assert.Equal(t, jsonAssistantRole, message[jsonRoleKey])
	assert.Equal(t, "using tool", message[jsonContentKey])
	calls, ok := message["tool_calls"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, calls, 1)
	assert.Equal(t, testCallID, calls[0]["id"])
}
