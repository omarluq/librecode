package llm_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

const mergeUsageProviderKey = "provider"

func TestMergeUsageOverlaysProviderReportedFields(t *testing.T) {
	t.Parallel()

	estimated := llm.Usage{
		Breakdown: map[string]int{"history": 100},
		TopContributors: []llm.TokenContributor{
			{Label: "estimate", Role: "user", Preview: "old", Tokens: 100, Chars: 300},
		},
		ContextWindow: 200_000,
		ContextTokens: 15_000,
		InputTokens:   15_000,
		OutputTokens:  0,
	}
	reported := llm.Usage{
		Breakdown: map[string]int{mergeUsageProviderKey: 1},
		TopContributors: []llm.TokenContributor{
			{Label: "reported", Role: "assistant", Preview: "new", Tokens: 10, Chars: 30},
		},
		ContextWindow: 128_000,
		ContextTokens: 12_000,
		InputTokens:   12_000,
		OutputTokens:  700,
	}

	merged := llm.MergeUsage(estimated, reported)

	assert.Equal(t, 128_000, merged.ContextWindow)
	assert.Equal(t, 12_000, merged.ContextTokens)
	assert.Equal(t, 12_000, merged.InputTokens)
	assert.Equal(t, 700, merged.OutputTokens)
	assert.Equal(t, estimated.Breakdown, merged.Breakdown)
	assert.Equal(t, estimated.TopContributors, merged.TopContributors)
}

func TestMergeUsagePreservesEstimatesWhenReportedValuesAreZero(t *testing.T) {
	t.Parallel()

	estimated := llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   200_000,
		ContextTokens:   15_000,
		InputTokens:     15_000,
		OutputTokens:    99,
	}

	merged := llm.MergeUsage(estimated, llm.EmptyUsage())

	assert.Equal(t, estimated, merged)
}

func TestMergeUsageClonesReportedMetadataWhenEstimateIsEmpty(t *testing.T) {
	t.Parallel()

	reported := llm.Usage{
		Breakdown: map[string]int{"history": 12},
		TopContributors: []llm.TokenContributor{
			{Label: "message", Role: "user", Preview: "hello", Tokens: 12, Chars: 40},
		},
		ContextWindow: 0,
		ContextTokens: 0,
		InputTokens:   0,
		OutputTokens:  0,
	}

	merged := llm.MergeUsage(llm.EmptyUsage(), reported)

	require.Equal(t, reported.Breakdown, merged.Breakdown)
	require.Equal(t, reported.TopContributors, merged.TopContributors)
	reported.Breakdown["history"] = 999
	reported.TopContributors[0].Label = "mutated"
	assert.Equal(t, 12, merged.Breakdown["history"])
	assert.Equal(t, "message", merged.TopContributors[0].Label)
}

func TestMergeUsageUsesReportedMetadataWhenEstimateMetadataNil(t *testing.T) {
	t.Parallel()

	reported := llm.Usage{
		Breakdown: map[string]int{mergeUsageProviderKey: 3},
		TopContributors: []llm.TokenContributor{
			{Label: mergeUsageProviderKey, Role: "assistant", Preview: "ok", Tokens: 3, Chars: 9},
		},
		ContextWindow: 0,
		ContextTokens: 0,
		InputTokens:   21,
		OutputTokens:  4,
	}

	merged := llm.MergeUsage(llm.Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}, reported)

	assert.Equal(t, map[string]int{mergeUsageProviderKey: 3}, merged.Breakdown)
	require.Len(t, merged.TopContributors, 1)
	assert.Equal(t, mergeUsageProviderKey, merged.TopContributors[0].Label)
	assert.Equal(t, 100, merged.ContextWindow)
	assert.Equal(t, 21, merged.InputTokens)
	assert.Equal(t, 4, merged.OutputTokens)
}
