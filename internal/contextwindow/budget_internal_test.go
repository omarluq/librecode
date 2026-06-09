package contextwindow

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/model"
)

func TestBudgetUsageWithReservesAndValidation(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100,
		ContextTokens:   90,
		InputTokens:     90,
		OutputTokens:    0,
	}
	budget := NewBudget(usage, nil, config.ContextConfig{
		OutputReserveTokens:   10,
		ProviderReserveTokens: 5,
		SafetyMarginTokens:    5,
		KeepRecentTokens:      0,
		PreflightEnabled:      true,
	}, func() int { return 5 })

	assert.Equal(t, 75, budget.UsableInput)
	assert.Equal(t, 25, budget.TotalReserve())
	enriched := budget.UsageWithBudget(usage)
	assert.Equal(t, 10, enriched.Breakdown["reserve_output"])
	assert.Equal(t, 5, enriched.Breakdown["reserve_tools"])
	assert.Equal(t, 75, enriched.Breakdown["usable_input"])

	err := budget.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "model context preflight failed")
}

func TestBudgetDefaultsAndReconstruction(t *testing.T) {
	t.Parallel()

	selectedModel := testModelWithContextWindow(1_000)
	budget := NewBudget(model.EmptyTokenUsage(), selectedModel, config.ContextConfig{
		OutputReserveTokens:   0,
		ProviderReserveTokens: -1,
		SafetyMarginTokens:    -1,
		KeepRecentTokens:      0,
		PreflightEnabled:      true,
	}, nil)

	assert.Equal(t, 1_000, budget.ContextWindow)
	assert.Equal(t, 200, budget.OutputReserve)
	assert.Equal(t, DefaultProviderReserve, budget.ProviderReserve)
	assert.Equal(t, DefaultSafetyMargin, budget.SafetyMargin)

	reconstructed := BudgetFromUsage(model.TokenUsage{
		Breakdown: map[string]int{
			"usable_input":     1,
			"reserve_output":   2,
			"reserve_tools":    3,
			"reserve_provider": 4,
			"reserve_safety":   5,
		},
		TopContributors: nil,
		ContextWindow:   10,
		ContextTokens:   6,
		InputTokens:     6,
		OutputTokens:    0,
	})

	assert.Equal(t, Budget{
		InputTokens:       6,
		ContextWindow:     10,
		UsableInput:       1,
		OutputReserve:     2,
		ToolSchemaReserve: 3,
		ProviderReserve:   4,
		SafetyMargin:      5,
	}, reconstructed)
}
