package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/llm"
)

const (
	testEstimatedUsageLabel = "estimated"
	testReportedUsageLabel  = "reported"
)

func TestMergeUsageKeepsExistingBreakdownWhenPresent(t *testing.T) {
	t.Parallel()

	estimated := llm.Usage{
		Breakdown: map[string]int{testEstimatedUsageLabel: 1}, ContextWindow: 10, ContextTokens: 2,
		TopContributors: []llm.TokenContributor{
			{Label: testEstimatedUsageLabel, Role: jsonSystemRole, Preview: "", Tokens: 1, Chars: 1},
		},
		InputTokens:  0,
		OutputTokens: 0,
	}
	reported := llm.Usage{
		Breakdown: map[string]int{testReportedUsageLabel: 2}, ContextWindow: 20, ContextTokens: 3,
		TopContributors: []llm.TokenContributor{
			{Label: testReportedUsageLabel, Role: jsonUserRole, Preview: "", Tokens: 2, Chars: 2},
		},
		InputTokens: 4, OutputTokens: 5,
	}

	merged := mergeUsage(estimated, reported)

	assert.Equal(t, map[string]int{testEstimatedUsageLabel: 1}, merged.Breakdown)
	assert.Equal(t, testEstimatedUsageLabel, merged.TopContributors[0].Label)
	assert.Equal(t, 20, merged.ContextWindow)
	assert.Equal(t, 3, merged.ContextTokens)
	assert.Equal(t, 4, merged.InputTokens)
	assert.Equal(t, 5, merged.OutputTokens)
}

func TestMergeUsageAccumulatesRequestTokens(t *testing.T) {
	t.Parallel()

	merged := accumulateUsage(
		llm.Usage{
			Breakdown: nil, TopContributors: nil, ContextWindow: 100, ContextTokens: 20,
			InputTokens: 7, OutputTokens: 3,
		},
		llm.Usage{
			Breakdown: nil, TopContributors: nil, ContextWindow: 100, ContextTokens: 30,
			InputTokens: 11, OutputTokens: 5,
		},
	)

	assert.Equal(t, 18, merged.InputTokens)
	assert.Equal(t, 8, merged.OutputTokens)
	assert.Equal(t, 30, merged.ContextTokens)
}

func TestUsageFromObjectIgnoresNonObjectsAndMissingValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, llm.EmptyUsage(), usageFromObject("not object"))
	assert.Equal(t, llm.EmptyUsage(), usageFromObject(map[string]any{"total_tokens": float64(10)}))
}

func TestIntFromAnyParsesSupportedTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 3, intFromAny(3))
	assert.Equal(t, 4, intFromAny(int64(4)))
	assert.Equal(t, 5, intFromAny(float64(5.9)))
	assert.Equal(t, 6, intFromAny(json.Number("6")))
	assert.Zero(t, intFromAny("7"))
}
