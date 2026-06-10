package llmconv_test

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/llmconv"
	"github.com/omarluq/librecode/internal/model"
)

func TestUsageConversionsCloneMapsAndContributors(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown: map[string]int{"history": 1},
		TopContributors: []model.TokenContributor{
			{Label: "history", Role: string(llm.RoleUser), Preview: "hello", Tokens: 2, Chars: 5},
		},
		ContextWindow: 10,
		ContextTokens: 2,
		InputTokens:   2,
		OutputTokens:  1,
	}

	converted := llmconv.UsageFromModel(usage)
	usage.Breakdown["history"] = 99
	usage.TopContributors[0].Label = "mutated"

	assert.Equal(t, 1, converted.Breakdown["history"])
	require.Len(t, converted.TopContributors, 1)
	assert.Equal(t, "history", converted.TopContributors[0].Label)

	roundTrip := llmconv.UsageToModel(converted)
	converted.Breakdown["history"] = 42
	converted.TopContributors[0].Label = "mutated again"

	assert.Equal(t, 1, roundTrip.Breakdown["history"])
	require.Len(t, roundTrip.TopContributors, 1)
	assert.Equal(t, "history", roundTrip.TopContributors[0].Label)
}

func TestTokenContributorConversionsPreserveNilAndClone(t *testing.T) {
	t.Parallel()

	assert.Nil(t, llmconv.TokenContributorsFromModel(nil))
	assert.Nil(t, llmconv.TokenContributorsToModel(nil))

	contributors := []model.TokenContributor{
		{Label: "message", Role: string(llm.RoleAssistant), Preview: "preview", Tokens: 4, Chars: 20},
	}

	converted := llmconv.TokenContributorsFromModel(contributors)
	require.Len(t, converted, 1)

	contributors[0].Label = "mutated"
	assert.Equal(t, "message", converted[0].Label)

	roundTrip := llmconv.TokenContributorsToModel(converted)
	require.Len(t, roundTrip, 1)

	converted[0].Label = "mutated again"
	assert.Equal(t, "message", roundTrip[0].Label)
}
