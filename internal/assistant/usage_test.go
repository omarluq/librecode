//nolint:testpackage // These tests exercise unexported usage parsing helpers.
package assistant

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
)

func TestUsageFromObjectParsesProviderShapes(t *testing.T) {
	t.Parallel()

	tests := []usageParseTest{
		{
			name: "openai responses",
			usage: map[string]any{
				"input_tokens":      float64(123),
				jsonOutputTokensKey: float64(45),
			},
			expected: model.TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: 123, OutputTokens: 45},
		},
		{
			name: "chat completions",
			usage: map[string]any{
				"prompt_tokens":     json.Number("77"),
				"completion_tokens": json.Number("9"),
			},
			expected: model.TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: 77, OutputTokens: 9},
		},
		{
			name: "total tokens does not become input tokens",
			usage: map[string]any{
				"total_tokens":      json.Number("120"),
				jsonOutputTokensKey: json.Number("20"),
			},
			expected: model.TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: 100, OutputTokens: 20},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.expected, usageFromObject(test.usage))
		})
	}
}

func TestMergeUsagePreservesEstimatedContextWindow(t *testing.T) {
	t.Parallel()

	estimated := model.TokenUsage{ContextWindow: 1000, ContextTokens: 200, InputTokens: 200, OutputTokens: 0}
	reported := model.TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: 150, OutputTokens: 25}

	assert.Equal(t, model.TokenUsage{
		ContextWindow: 1000,
		ContextTokens: 200,
		InputTokens:   150,
		OutputTokens:  25,
	}, mergeUsage(estimated, reported))
}

func TestMergeUsageNeverShrinksEstimatedContext(t *testing.T) {
	t.Parallel()

	estimated := model.TokenUsage{ContextWindow: 100_000, ContextTokens: 14_000, InputTokens: 14_000, OutputTokens: 0}
	reported := model.TokenUsage{ContextWindow: 0, ContextTokens: 12_000, InputTokens: 12_000, OutputTokens: 700}

	assert.Equal(t, model.TokenUsage{
		ContextWindow: 100_000,
		ContextTokens: 14_000,
		InputTokens:   12_000,
		OutputTokens:  700,
	}, mergeUsage(estimated, reported))
}

func TestMergeUsageDoesNotPromoteProviderTotalToContext(t *testing.T) {
	t.Parallel()

	estimated := model.TokenUsage{ContextWindow: 272_000, ContextTokens: 0, InputTokens: 0, OutputTokens: 0}
	reported := model.TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: 13_000_000, OutputTokens: 100}

	assert.Equal(t, model.TokenUsage{
		ContextWindow: 272_000,
		ContextTokens: 0,
		InputTokens:   13_000_000,
		OutputTokens:  100,
	}, mergeUsage(estimated, reported))
}

func TestParseSSEResultPreservesUsageWhenItemsProvideText(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`data: {"response":{"usage":{"input_tokens":12,"output_tokens":7}}}`,
		`data: {"item":{"id":"msg_1","type":"message","content":[{"type":"output_text","text":"hello"}]}}`,
		`data: [DONE]`,
		``,
	}, "\n")

	result, err := parseSSEResult(strings.NewReader(stream), nil)
	require.NoError(t, err)
	assert.Equal(t, model.TokenUsage{
		ContextWindow: 0,
		ContextTokens: 0,
		InputTokens:   12,
		OutputTokens:  7,
	}, result.Usage)
	assert.Equal(t, "hello", result.Text)
}

type usageParseTest struct {
	usage    map[string]any
	name     string
	expected model.TokenUsage
}
