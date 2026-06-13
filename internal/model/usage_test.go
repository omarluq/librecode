package model_test

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
)

func TestTokenUsageHelpers(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   25,
		InputTokens:     10,
		OutputTokens:    15,
	}

	assert.Equal(t, 25, usage.TotalTokens())
	assert.True(t, usage.HasAny())
	assert.Equal(t, 25, usage.ContextPercent())
}

func TestTokenUsageContextPercentCapsAtHundred(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   250,
		InputTokens:     0,
		OutputTokens:    0,
	}

	assert.Equal(t, 100, usage.ContextPercent())
}

func TestTokenUsageReadsLegacyTopContributors(t *testing.T) {
	t.Parallel()

	var usage model.TokenUsage

	err := json.Unmarshal([]byte(`{"topContributors":[{"label":"history","tokens":12,"chars":40}]}`), &usage)
	require.NoError(t, err)
	require.Len(t, usage.TopContributors, 1)
	assert.Equal(t, "history", usage.TopContributors[0].Label)
}

func TestEmptyTokenUsageHasNoUsage(t *testing.T) {
	t.Parallel()

	usage := model.EmptyTokenUsage()

	assert.False(t, usage.HasAny())
	assert.Equal(t, 0, usage.TotalTokens())
	assert.Equal(t, 0, usage.ContextPercent())
}
