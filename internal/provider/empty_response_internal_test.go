package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestFinishProviderResultAllowsEmptyText(t *testing.T) {
	t.Parallel()

	result := &llm.Response{
		FinishReason: llm.FinishReasonUnknown,
		Content:      nil,
		ToolCalls:    nil,
		Usage:        llm.EmptyUsage(),
	}
	finished, err := finishProviderResult(result, &providerResult{
		FinishReason: llm.FinishReasonStop,
		Text:         strings.Repeat(" ", 3),
		OutputItems:  nil,
		Thinking:     nil,
		ToolCalls:    nil,
		Usage:        llm.EmptyUsage(),
	})

	require.NoError(t, err)
	assert.True(t, finished)
	assert.Empty(t, responseText(result))
}

func TestProviderParsersAllowSuccessfulEmptyResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		parse func([]byte) (*providerResult, error)
		name  string
		body  string
	}{
		{
			name:  "openai chat no choices",
			body:  `{"usage":{"prompt_tokens":7,"completion_tokens":0,"total_tokens":7}}`,
			parse: parseOpenAIChatResult,
		},
		{
			name:  "openai chat blank content",
			body:  `{"choices":[{"message":{"content":"   "}}]}`,
			parse: parseOpenAIChatResult,
		},
		{
			name:  "openai responses blank output",
			body:  `{"output":[],"usage":{"input_tokens":5,"output_tokens":0,"total_tokens":5}}`,
			parse: parseOpenAIResponseResult,
		},
		{
			name:  "anthropic empty content",
			body:  `{"content":[],"usage":{"input_tokens":3,"output_tokens":0}}`,
			parse: parseAnthropicResult,
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := testCase.parse(json.RawMessage(testCase.body))

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Empty(t, result.Text)
			assert.Empty(t, result.ToolCalls)
		})
	}
}
