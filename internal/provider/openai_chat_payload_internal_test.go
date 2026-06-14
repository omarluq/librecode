package provider

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestOpenAIChatPayloadAndRoles(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth(testOpenAIProvider, "sk-test")
	setTestRequestModelID(request, "gpt-test")
	setTestRequestReasoning(request, true)
	setTestRequestThinkingLevel(request, thinkingHigh)
	payload := openAIChatPayload(request, nil)

	assert.Equal(t, "gpt-test", payload[jsonModelKey])
	assert.Equal(t, thinkingHigh, payload["reasoning_effort"])
	assert.Equal(t, "auto", payload[jsonToolChoiceKey])
	assert.NotEmpty(t, payload["tools"])
}

func TestOpenAIChatPayloadMapsReasoningEffort(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth(testOpenAIProvider, "sk-test")
	setTestRequestReasoning(request, true)
	setTestRequestThinkingLevel(request, thinkingXHigh)
	setTestThinkingMap(request, thinkingXHigh, "max")

	payload := openAIChatPayload(request, nil)

	assert.Equal(t, "max", payload["reasoning_effort"])
}

func TestOpenAIChatMessagesAndRoles(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth(testOpenAIProvider, "sk-test")
	setTestRequestSystemPrompt(request, "system")
	setTestRequestMessages(request, mixedReplayMessages())

	messages := openAIChatMessages(request)

	assert.Len(t, messages, 8)
	assert.JSONEq(t, jsonString(jsonSystemRole), jsonString(messages[0][jsonRoleKey]))
	assert.JSONEq(t, jsonString(jsonUserRole), jsonString(messages[1][jsonRoleKey]))
	assert.JSONEq(t, jsonString(jsonAssistantRole), jsonString(messages[2][jsonRoleKey]))

	mapped, ok := openAIRole(llm.RoleTool)
	assert.False(t, ok)
	assert.Empty(t, mapped)
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
	assert.Equal(t, llm.FinishReasonToolCalls, result.FinishReason)
	require.Len(t, result.ToolCalls, 1)
	assert.Equal(t, testCallID, result.ToolCalls[0].ID)
	assert.Equal(t, testToolPath, result.ToolCalls[0].Arguments[jsonPathKey])
	assert.Equal(t, llm.Usage{
		Breakdown: nil, TopContributors: nil, ContextWindow: 0, ContextTokens: 0,
		InputTokens: 8, OutputTokens: 2,
	}, result.Usage)
}

func TestParseOpenAIChatResultMapsFinishReasonLength(t *testing.T) {
	t.Parallel()

	result, err := parseOpenAIChatResult([]byte(
		`{"choices":[{"finish_reason":"length","message":{"content":"partial"}}]}`,
	))

	require.NoError(t, err)
	assert.Equal(t, "partial", result.Text)
	assert.Equal(t, llm.FinishReasonLength, result.FinishReason)
}

func TestOpenAIChatAssistantToolMessage(t *testing.T) {
	t.Parallel()

	message := openAIChatAssistantToolMessage(&providerResult{
		FinishReason: llm.FinishReasonToolCalls,
		Text:         "using tool",
		OutputItems:  nil,
		Thinking:     nil,
		ToolCalls: []ToolCall{{
			Arguments:     nil,
			Metadata:      nil,
			ID:            testCallID,
			Name:          jsonReadToolName,
			ArgumentsJSON: testToolArgumentsJSON,
			TextFallback:  false,
		}},
		Usage: llm.EmptyUsage(),
	})

	assert.JSONEq(t, jsonString(jsonAssistantRole), jsonString(message[jsonRoleKey]))
	assert.Equal(t, "using tool", message[jsonContentKey])
	calls, ok := message["tool_calls"].([]map[string]any)
	require.True(t, ok)
	require.Len(t, calls, 1)
	assert.Equal(t, testCallID, calls[0]["id"])
}
