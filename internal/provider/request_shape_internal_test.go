package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProviderRequestShapeCapturesSafeMetadata(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input    any
		messages any
		tools    any
		name     string
	}{
		{
			name: "generic JSON slices",
			input: []any{
				map[string]any{jsonTypeKey: functionCallType},
				map[string]any{jsonTypeKey: functionCallOutputType},
			},
			messages: []any{map[string]any{}},
			tools:    []any{map[string]any{}},
		},
		{
			name: "typed payload slices",
			input: []map[string]any{
				{jsonTypeKey: functionCallType},
				{jsonTypeKey: functionCallOutputType},
			},
			messages: []map[string]any{{}},
			tools:    []map[string]any{{}},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			shape := providerRequestShape(map[string]any{
				requestShapeIncludeKey:           []string{reasoningContentKey},
				jsonInputKey:                     test.input,
				jsonMessagesKey:                  test.messages,
				requestShapeParallelToolCallsKey: true,
				requestShapePromptCacheKey:       "session-1",
				jsonReasoningKey:                 map[string]any{reasoningEffortKey: thinkingLow},
				jsonToolsKey:                     test.tools,
			})

			assert.True(t, shape.HasInclude)
			assert.True(t, shape.HasParallelToolCalls)
			assert.True(t, shape.HasPromptCacheKey)
			assert.True(t, shape.HasReasoning)
			assert.Equal(t, 2, shape.InputCount)
			assert.Equal(t, 1, shape.FunctionCallCount)
			assert.Equal(t, 1, shape.FunctionCallOutputCount)
			assert.Equal(t, 1, shape.MessageCount)
			assert.Equal(t, 1, shape.ToolCount)
			assert.NotZero(t, shape.ByteSize)
			assert.Equal(t, []string{
				requestShapeIncludeKey,
				jsonInputKey,
				jsonMessagesKey,
				requestShapeParallelToolCallsKey,
				requestShapePromptCacheKey,
				jsonReasoningKey,
				jsonToolsKey,
			}, shape.Keys)

			payload := shape.Payload()
			assert.Equal(t, true, payload["has_include"])
			assert.Equal(t, 2, payload[requestShapeInputCountKey])
		})
	}
}

func TestProviderRequestShapeReportsFalsyOptionalFields(t *testing.T) {
	t.Parallel()

	tests := []struct {
		payload map[string]any
		name    string
	}{
		{
			name: "disabled parallel tool calls",
			payload: map[string]any{
				requestShapeParallelToolCallsKey: false,
			},
		},
		{
			name: "blank prompt cache key",
			payload: map[string]any{
				requestShapePromptCacheKey: "  ",
			},
		},
		{
			name: "empty include list",
			payload: map[string]any{
				requestShapeIncludeKey: []string{},
			},
		},
		{
			name: "empty reasoning object",
			payload: map[string]any{
				jsonReasoningKey: map[string]any{},
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			shape := providerRequestShape(test.payload)

			assert.False(t, shape.HasParallelToolCalls)
			assert.False(t, shape.HasPromptCacheKey)
			assert.False(t, shape.HasInclude)
			assert.False(t, shape.HasReasoning)
		})
	}
}

func TestProviderRequestShapeHandlesEmptyPayload(t *testing.T) {
	t.Parallel()

	assert.True(t, providerRequestShape(nil).empty())
	assert.Empty(t, providerRequestShape(nil).Payload())
}
