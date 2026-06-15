package provider

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestUsageFromObjectParsesProviderShapes(t *testing.T) {
	t.Parallel()

	tests := []usageParseTest{
		{
			name: "openai responses",
			usage: map[string]any{
				jsonInputTokensKey:  float64(123),
				jsonOutputTokensKey: float64(45),
			},
			expected: llm.Usage{
				Breakdown: nil, ContextWindow: 0, ContextTokens: 0,
				TopContributors: nil,
				InputTokens:     123, OutputTokens: 45,
			},
		},
		{
			name: "chat completions",
			usage: map[string]any{
				"prompt_tokens":     json.Number("77"),
				"completion_tokens": json.Number("9"),
			},
			expected: llm.Usage{
				Breakdown: nil, ContextWindow: 0, ContextTokens: 0,
				TopContributors: nil,
				InputTokens:     77, OutputTokens: 9,
			},
		},
		{
			name: "total tokens does not become input tokens",
			usage: map[string]any{
				"total_tokens":      json.Number("120"),
				jsonOutputTokensKey: json.Number("20"),
			},
			expected: llm.Usage{
				Breakdown: nil, ContextWindow: 0, ContextTokens: 0,
				TopContributors: nil,
				InputTokens:     100, OutputTokens: 20,
			},
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

	estimated := usageFixture(1000, 200, 200, 0)
	reported := usageFixture(0, 0, 150, 25)

	assert.Equal(t, llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1000,
		ContextTokens:   200,
		InputTokens:     150,
		OutputTokens:    25,
	}, mergeUsage(estimated, reported))
}

func TestMergeUsageAcceptsProviderContextTokens(t *testing.T) {
	t.Parallel()

	estimated := usageFixture(100_000, 14_000, 14_000, 0)
	reported := usageFixture(0, 12_000, 12_000, 700)

	assert.Equal(t, llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   12_000,
		InputTokens:     12_000,
		OutputTokens:    700,
	}, mergeUsage(estimated, reported))
}

func TestMergeUsageDoesNotPromoteProviderTotalToContext(t *testing.T) {
	t.Parallel()

	estimated := usageFixture(272_000, 0, 0, 0)
	reported := usageFixture(0, 0, 13_000_000, 100)

	assert.Equal(t, llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   272_000,
		ContextTokens:   0,
		InputTokens:     13_000_000,
		OutputTokens:    100,
	}, mergeUsage(estimated, reported))
}

func TestMergeUsageClonesReportedContributors(t *testing.T) {
	t.Parallel()

	reported := llm.Usage{
		Breakdown:     map[string]int{"history": 12},
		ContextWindow: 0,
		ContextTokens: 0,
		TopContributors: []llm.TokenContributor{
			{Label: "message 1", Role: jsonUserRole, Preview: "usage contributor preview", Tokens: 10, Chars: 40},
		},
		InputTokens:  0,
		OutputTokens: 0,
	}

	merged := mergeUsage(llm.EmptyUsage(), reported)
	reported.TopContributors[0].Label = "changed contributor"

	assert.Equal(t, "message 1", merged.TopContributors[0].Label)
}

func TestIntFromAnyIgnoresInvalidJSONNumber(t *testing.T) {
	t.Parallel()

	assert.Zero(t, intFromAny(json.Number("not-a-number")))
}

func TestParseSSEResultPreservesUsageWhenItemsProvideText(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`data: {"response":{"usage":{"input_tokens":12,"output_tokens":7}}}`,
		`data: {"item":{"id":"msg_1","type":"message","content":[{"type":"output_text","text":"hello"}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1"}}`,
		``,
	}, "\n")

	result, err := parseSSEResult(strings.NewReader(stream), nil)
	require.NoError(t, err)
	assert.Equal(t, llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     12,
		OutputTokens:    7,
	}, result.Usage)
	assert.Equal(t, "hello", result.Text)
}

func TestParseSSEResultPreservesUsageAcrossLaterResponseEvents(t *testing.T) {
	t.Parallel()

	stream := strings.Join([]string{
		`data: {"usage":{"input_tokens":12,"output_tokens":7}}`,
		`data: {"response":{"output":[{"id":"msg_1","type":"message",` +
			`"content":[{"type":"output_text","text":"hello"}]}]}}`,
		`data: {"type":"response.completed","response":{"id":"resp_1"}}`,
		``,
	}, "\n")

	result, err := parseSSEResult(strings.NewReader(stream), nil)
	require.NoError(t, err)
	assert.Equal(t, llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     12,
		OutputTokens:    7,
	}, result.Usage)
	assert.Equal(t, "hello", result.Text)
}

type usageParseTest struct {
	usage    map[string]any
	name     string
	expected llm.Usage
}

func usageFixture(contextWindow, contextTokens, inputTokens, outputTokens int) llm.Usage {
	return llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   contextWindow,
		ContextTokens:   contextTokens,
		InputTokens:     inputTokens,
		OutputTokens:    outputTokens,
	}
}
