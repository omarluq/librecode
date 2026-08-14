package contextwindow

import (
	"math"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestSummaryOutputLimit(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name                 string
		reserve, model, want int
		wantErr              bool
	}{
		{"reserve cap", 10_000, 20_000, 8_000, false},
		{"model cap", 10_000, 2_000, 2_000, false},
		{"unknown model", 10_000, 0, 4_096, false},
		{"floor integer", 333, 10_000, 266, false},
		{"exact minimum", 80, 10_000, 64, false},
		{"below minimum", 79, 10_000, 0, true},
		{"invalid", 0, 10_000, 0, true},
		{"maximum integer", math.MaxInt, 10_000, 0, true},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, err := SummaryOutputLimit(testCase.reserve, testCase.model)
			if testCase.wantErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
			}

			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestSummaryBudgetValidation(t *testing.T) {
	t.Parallel()

	budget := NewSummaryBudget(SummaryBudgetInput{ContextWindow: 1000, SystemPromptTokens: 100,
		PreviousSummaryTokens: 40, SplitTurnTokens: 20, HistoryTokens: 500,
		ProviderReserve: 50, SafetyMargin: 50, EnvelopeTokens: 25, MaxOutputTokens: 200})
	assert.Equal(t, 125, budget.FixedInputTokens)
	assert.Equal(t, 575, budget.AvailableHistoryTokens())
	assert.Equal(t, 625, budget.TotalInputTokens)
	require.NoError(t, budget.Validate())

	budget.ReducibleHistoryTokens = 600
	budget.TotalInputTokens = budget.FixedInputTokens + budget.ReducibleHistoryTokens
	require.Error(t, budget.Validate())
}
