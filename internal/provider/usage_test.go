//nolint:testpackage // These tests exercise unexported usage parsing helpers.
package provider

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
			expected: model.TokenUsage{
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
			expected: model.TokenUsage{
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
			expected: model.TokenUsage{
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

	estimated := model.TokenUsage{
		Breakdown: nil, ContextWindow: 1000, ContextTokens: 200,
		TopContributors: nil,
		InputTokens:     200, OutputTokens: 0,
	}
	reported := model.TokenUsage{
		Breakdown: nil, ContextWindow: 0, ContextTokens: 0,
		TopContributors: nil,
		InputTokens:     150, OutputTokens: 25,
	}

	assert.Equal(t, model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1000,
		ContextTokens:   200,
		InputTokens:     150,
		OutputTokens:    25,
	}, mergeUsage(estimated, reported))
}

func TestMergeUsageNeverShrinksEstimatedContext(t *testing.T) {
	t.Parallel()

	estimated := model.TokenUsage{
		Breakdown: nil, ContextWindow: 100_000, ContextTokens: 14_000,
		TopContributors: nil,
		InputTokens:     14_000, OutputTokens: 0,
	}
	reported := model.TokenUsage{
		Breakdown: nil, ContextWindow: 0, ContextTokens: 12_000,
		TopContributors: nil,
		InputTokens:     12_000, OutputTokens: 700,
	}

	assert.Equal(t, model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   14_000,
		InputTokens:     12_000,
		OutputTokens:    700,
	}, mergeUsage(estimated, reported))
}

func TestMergeUsageDoesNotPromoteProviderTotalToContext(t *testing.T) {
	t.Parallel()

	estimated := model.TokenUsage{
		Breakdown: nil, ContextWindow: 272_000, ContextTokens: 0,
		TopContributors: nil,
		InputTokens:     0, OutputTokens: 0,
	}
	reported := model.TokenUsage{
		Breakdown: nil, ContextWindow: 0, ContextTokens: 0,
		TopContributors: nil,
		InputTokens:     13_000_000, OutputTokens: 100,
	}

	assert.Equal(t, model.TokenUsage{
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

	reported := model.TokenUsage{
		Breakdown: map[string]int{"history": 12}, ContextWindow: 0, ContextTokens: 0,
		TopContributors: []model.TokenContributor{
			{Label: "message 1", Role: "user", Preview: "usage contributor preview", Tokens: 10, Chars: 40},
		},
		InputTokens: 0, OutputTokens: 0,
	}

	merged := mergeUsage(model.EmptyTokenUsage(), reported)
	reported.TopContributors[0].Label = "changed contributor"

	assert.Equal(t, "message 1", merged.TopContributors[0].Label)
}

func TestEstimateTokensHandlesEmptyText(t *testing.T) {
	t.Parallel()

	assert.Zero(t, estimateTokens(" \n\t "))
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
	assert.Equal(t, model.TokenUsage{
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
	assert.Equal(t, model.TokenUsage{
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
	expected model.TokenUsage
}
