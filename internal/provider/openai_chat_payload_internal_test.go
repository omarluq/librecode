package provider

import (
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
	payload := openAIChatPayload(request)

	assert.Equal(t, "gpt-test", payload[jsonModelKey])
	assert.Equal(t, thinkingHigh, payload["reasoning_effort"])
	assert.Equal(t, "auto", payload[jsonToolChoiceKey])
	assert.Equal(t, true, payload[jsonStreamKey])
	assert.Equal(t, map[string]any{"include_usage": true}, payload["stream_options"])
	assert.NotEmpty(t, payload["tools"])
}

func TestOpenAIChatPayloadMapsReasoningEffort(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth(testOpenAIProvider, "sk-test")
	setTestRequestReasoning(request, true)
	setTestRequestThinkingLevel(request, thinkingXHigh)
	setTestThinkingMap(request, thinkingXHigh, "max")

	payload := openAIChatPayload(request)

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

func TestOpenAIChatPayloadAddsZAIStreamingOptions(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("zai", "sk-test")
	setTestRequestModelID(request, "glm-5.2")
	setTestRequestReasoning(request, true)
	setTestRequestThinkingLevel(request, thinkingXHigh)
	setTestThinkingMap(request, thinkingXHigh, "max")

	payload := openAIChatPayload(request)

	assert.Equal(t, true, payload[jsonStreamKey])
	assert.Equal(t, "max", payload["reasoning_effort"])
	assert.Equal(t, map[string]any{jsonTypeKey: thinkingEnabled}, payload[jsonThinkingKey])
	assert.Equal(t, true, payload["tool_stream"])
}

func TestOpenAIChatPayloadDisablesZAIThinkingAndOmitsToolStreamWithoutTools(t *testing.T) {
	t.Parallel()

	request := testCompletionRequestAuth("zai", "sk-test")
	setTestRequestReasoning(request, true)
	setTestRequestThinkingLevel(request, thinkingOff)
	request.Request.DisableTools = true

	payload := openAIChatPayload(request)

	assert.Equal(t, map[string]any{jsonTypeKey: thinkingDisabled}, payload[jsonThinkingKey])
	assert.NotContains(t, payload, "tool_stream")
	assert.NotContains(t, payload, "reasoning_effort")
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
		}},
		Usage: llm.EmptyUsage(),
	})

	assert.JSONEq(t, jsonString(jsonAssistantRole), jsonString(message[jsonRoleKey]))
	assert.Equal(t, "using tool", message[jsonContentKey])
	calls, ok := message[jsonToolCallsKey].([]map[string]any)
	require.True(t, ok)
	require.Len(t, calls, 1)
	assert.Equal(t, testCallID, calls[0]["id"])
}
