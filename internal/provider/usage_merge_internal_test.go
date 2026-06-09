package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
)

const (
	testEstimatedUsageLabel = "estimated"
	testReportedUsageLabel  = "reported"
)

func TestEstimateTokensCountsRunesConservatively(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 1, estimateTokens("a"))
	assert.Equal(t, 1, estimateTokens("abcd"))
	assert.Equal(t, 2, estimateTokens("abcde"))
	assert.Equal(t, 1, estimateTokens("🙂"))
}

func TestMergeUsageKeepsExistingBreakdownWhenPresent(t *testing.T) {
	t.Parallel()

	estimated := model.TokenUsage{
		Breakdown: map[string]int{testEstimatedUsageLabel: 1}, ContextWindow: 10, ContextTokens: 2,
		TopContributors: []model.TokenContributor{
			{Label: testEstimatedUsageLabel, Role: jsonSystemRole, Preview: "", Tokens: 1, Chars: 1},
		},
		InputTokens: 0, OutputTokens: 0,
	}
	reported := model.TokenUsage{
		Breakdown: map[string]int{testReportedUsageLabel: 2}, ContextWindow: 20, ContextTokens: 3,
		TopContributors: []model.TokenContributor{
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

func TestUsageFromObjectIgnoresNonObjectsAndMissingValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, model.EmptyTokenUsage(), usageFromObject("not object"))
	assert.Equal(t, model.EmptyTokenUsage(), usageFromObject(map[string]any{"total_tokens": float64(10)}))
}

func TestIntFromAnyParsesSupportedTypes(t *testing.T) {
	t.Parallel()

	assert.Equal(t, 3, intFromAny(3))
	assert.Equal(t, 4, intFromAny(int64(4)))
	assert.Equal(t, 5, intFromAny(float64(5.9)))
	assert.Equal(t, 6, intFromAny(json.Number("6")))
	assert.Zero(t, intFromAny("7"))
}
