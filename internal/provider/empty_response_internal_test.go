package provider

import (
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
	assert.Empty(t, result.Content)
	assert.Empty(t, responseText(result))
}

func TestStreamingParsersAllowSuccessfulEmptyResponses(t *testing.T) {
	t.Parallel()

	tests := []struct {
		parse func() (*providerResult, error)
		name  string
	}{
		{
			name: "openai chat blank content",
			parse: func() (*providerResult, error) {
				return parseOpenAIChatStream(strings.NewReader(openAIChatStream(openAIChatChunk(map[string]any{
					jsonChoicesKey: []any{map[string]any{
						anthropicDeltaKey:   map[string]any{jsonContentKey: strings.Repeat(" ", 3)},
						jsonFinishReasonKey: "stop",
					}},
				}))), nil)
			},
		},
		{
			name: "openai responses blank output",
			parse: func() (*providerResult, error) {
				return parseSSEResult(strings.NewReader(openAIResponseCompletedStream(`{
					"output": [],
					"usage": {"input_tokens": 5, "output_tokens": 0, "total_tokens": 5}
				}`)), nil)
			},
		},
		{
			name: "anthropic empty content",
			parse: func() (*providerResult, error) {
				return parseAnthropicStream(strings.NewReader(strings.Join([]string{
					anthropicEventMessageStart,
					anthropicDataLine(map[string]any{
						jsonTypeKey: anthropicMessageStartEvent,
						jsonMessageKey: map[string]any{
							jsonUsageKey: map[string]any{jsonInputTokensKey: 3, jsonOutputTokensKey: 0},
						},
					}),
					"",
					anthropicEventMessageStop,
					anthropicMessageStopData,
					"",
				}, "\n")), nil)
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			result, err := testCase.parse()

			require.NoError(t, err)
			require.NotNil(t, result)
			assert.Empty(t, result.Text)
			assert.Empty(t, result.ToolCalls)
		})
	}
}
