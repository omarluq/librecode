package contextwindow

import (
	"errors"
	"fmt"
	"math"
)

const (
	// SummaryReservePercent is the percentage of output reserve available to summaries.
	SummaryReservePercent = 80
	// DefaultUnknownModelSummaryMaxTokens limits summaries for models without a declared output limit.
	DefaultUnknownModelSummaryMaxTokens = 4_096
	// MinimumSummaryOutputTokens is the smallest viable summary output allowance.
	MinimumSummaryOutputTokens = 64
	// DefaultSummaryRequestEnvelopeTokens reserves request wrapper overhead.
	DefaultSummaryRequestEnvelopeTokens = 32
	percentScale                        = 100
)

// SummaryOutputLimit computes floor(80% of reserve), bounded by model capability.
func SummaryOutputLimit(outputReserve, modelMaxTokens int) (int, error) {
	if outputReserve <= 0 || modelMaxTokens < 0 {
		return 0, fmt.Errorf("invalid summary output bounds: reserve=%d model_max=%d", outputReserve, modelMaxTokens)
	}

	if uint64(outputReserve) > uint64(math.MaxInt64)/SummaryReservePercent {
		return 0, errors.New("summary output bound overflows int64")
	}

	reserveCap64 := int64(outputReserve) * SummaryReservePercent / int64(percentScale)
	if reserveCap64 > int64(math.MaxInt) {
		return 0, errors.New("summary output bound overflows int")
	}

	modelCap := modelMaxTokens
	if modelCap == 0 {
		modelCap = DefaultUnknownModelSummaryMaxTokens
	}

	limit := min(int(reserveCap64), modelCap)
	if limit < MinimumSummaryOutputTokens {
		return 0, fmt.Errorf(
			"summary output limit %d is below minimum viable allowance %d",
			limit,
			MinimumSummaryOutputTokens,
		)
	}

	return limit, nil
}

// SummaryBudgetInput contains every summary-request budget component. Previous
// and split tokens are diagnostic portions of SystemPromptTokens, not additions.
type SummaryBudgetInput struct {
	ContextWindow         int
	SystemPromptTokens    int
	PreviousSummaryTokens int
	SplitTurnTokens       int
	HistoryTokens         int
	ProviderReserve       int
	SafetyMargin          int
	EnvelopeTokens        int
	MaxOutputTokens       int
}

// SummaryBudget is a tool-free, finite compaction request budget.
type SummaryBudget struct {
	ContextWindow          int
	SystemPromptTokens     int
	PreviousSummaryTokens  int
	SplitTurnTokens        int
	FixedInputTokens       int
	ReducibleHistoryTokens int
	MaxInputTokens         int
	TotalInputTokens       int
	ProviderReserve        int
	SafetyMargin           int
	EnvelopeTokens         int
	MaxOutputTokens        int
}

// NewSummaryBudget computes the complete request budget.
func NewSummaryBudget(input SummaryBudgetInput) SummaryBudget {
	fixed := max(input.SystemPromptTokens, 0) + max(input.EnvelopeTokens, 0)
	maxInput := input.ContextWindow - max(input.MaxOutputTokens, 0) -
		max(input.ProviderReserve, 0) - max(input.SafetyMargin, 0)

	return SummaryBudget{
		ContextWindow:          input.ContextWindow,
		SystemPromptTokens:     input.SystemPromptTokens,
		PreviousSummaryTokens:  input.PreviousSummaryTokens,
		SplitTurnTokens:        input.SplitTurnTokens,
		FixedInputTokens:       fixed,
		ReducibleHistoryTokens: max(input.HistoryTokens, 0),
		MaxInputTokens:         maxInput,
		TotalInputTokens:       fixed + max(input.HistoryTokens, 0),
		ProviderReserve:        input.ProviderReserve,
		SafetyMargin:           input.SafetyMargin,
		EnvelopeTokens:         input.EnvelopeTokens,
		MaxOutputTokens:        input.MaxOutputTokens,
	}
}

// AvailableHistoryTokens returns input room after non-reducible overhead.
func (budget *SummaryBudget) AvailableHistoryTokens() int {
	return max(budget.MaxInputTokens-budget.FixedInputTokens, 0)
}

// Validate reports fixed or reducible overflow without sending a request.
func (budget *SummaryBudget) Validate() error {
	if budget.ContextWindow <= 0 || budget.MaxOutputTokens <= 0 || budget.MaxInputTokens <= 0 ||
		budget.FixedInputTokens > budget.MaxInputTokens {
		return fmt.Errorf(
			"summary fixed overhead exceeds budget: fixed=%d max_input=%d",
			budget.FixedInputTokens,
			budget.MaxInputTokens,
		)
	}

	if budget.TotalInputTokens > budget.MaxInputTokens {
		return fmt.Errorf(
			"summary input exceeds budget: input=%d max_input=%d",
			budget.TotalInputTokens,
			budget.MaxInputTokens,
		)
	}

	return nil
}
