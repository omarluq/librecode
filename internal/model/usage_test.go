package model_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
)

func TestTokenUsageHelpers(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 100,
		ContextTokens: 25,
		InputTokens:   10,
		OutputTokens:  15,
	}

	assert.Equal(t, 25, usage.TotalTokens())
	assert.True(t, usage.HasAny())
	assert.Equal(t, 25, usage.ContextPercent())
}

func TestTokenUsageContextPercentCapsAtHundred(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 100,
		ContextTokens: 250,
		InputTokens:   0,
		OutputTokens:  0,
	}

	assert.Equal(t, 100, usage.ContextPercent())
}

func TestEmptyTokenUsageHasNoUsage(t *testing.T) {
	t.Parallel()

	usage := model.EmptyTokenUsage()

	assert.False(t, usage.HasAny())
	assert.Equal(t, 0, usage.TotalTokens())
	assert.Equal(t, 0, usage.ContextPercent())
}
