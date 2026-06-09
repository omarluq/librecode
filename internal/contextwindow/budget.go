package contextwindow

import (
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/model"
)

const (
	// DefaultOutputReserve is the fallback output token reserve.
	DefaultOutputReserve = 16_384
	// DefaultSafetyMargin is the fallback safety reserve.
	DefaultSafetyMargin = 8_192
	// DefaultProviderReserve is the fallback provider overhead reserve.
	DefaultProviderReserve = 2_048
	// ReservePercent caps the default output reserve as a percentage of context window.
	ReservePercent = 20
)

// ToolSchemaEstimator estimates the token cost of available tool definitions.
type ToolSchemaEstimator func() int

// Budget describes usable context after output, provider, safety, and tool reserves.
type Budget struct {
	InputTokens       int
	ContextWindow     int
	UsableInput       int
	OutputReserve     int
	ToolSchemaReserve int
	ProviderReserve   int
	SafetyMargin      int
}

// NewBudget builds a context budget from current usage and policy.
func NewBudget(
	usage model.TokenUsage,
	selectedModel *model.Model,
	policy config.ContextConfig,
	estimateTools ToolSchemaEstimator,
) Budget {
	contextWindow := usage.ContextWindow
	if contextWindow <= 0 && selectedModel != nil {
		contextWindow = selectedModel.ContextWindow
	}
	budget := Budget{
		InputTokens:       usage.ContextTokens,
		ContextWindow:     contextWindow,
		UsableInput:       0,
		OutputReserve:     OutputReserve(selectedModel, contextWindow, policy),
		ToolSchemaReserve: estimateToolSchemaTokens(estimateTools),
		ProviderReserve:   nonNegativeOrDefault(policy.ProviderReserveTokens, DefaultProviderReserve),
		SafetyMargin:      nonNegativeOrDefault(policy.SafetyMarginTokens, DefaultSafetyMargin),
	}
	budget.UsableInput = max(contextWindow-budget.TotalReserve(), 0)

	return budget
}

func estimateToolSchemaTokens(estimator ToolSchemaEstimator) int {
	if estimator == nil {
		return 0
	}

	return estimator()
}

// TotalReserve returns the tokens reserved from the context window.
func (budget Budget) TotalReserve() int {
	return budget.OutputReserve + budget.ToolSchemaReserve + budget.ProviderReserve + budget.SafetyMargin
}

// UsageWithBudget adds budget diagnostics to usage.
func (budget Budget) UsageWithBudget(usage model.TokenUsage) model.TokenUsage {
	usage.ContextWindow = budget.ContextWindow
	usage.ContextTokens = budget.InputTokens
	usage.InputTokens = budget.InputTokens
	if usage.Breakdown == nil {
		usage.Breakdown = map[string]int{}
	}
	usage.Breakdown["reserve_output"] = budget.OutputReserve
	usage.Breakdown["reserve_tools"] = budget.ToolSchemaReserve
	usage.Breakdown["reserve_provider"] = budget.ProviderReserve
	usage.Breakdown["reserve_safety"] = budget.SafetyMargin
	usage.Breakdown["usable_input"] = budget.UsableInput

	return usage
}

// Validate reports whether estimated input fits inside usable context.
func (budget Budget) Validate() error {
	if budget.ContextWindow <= 0 || budget.InputTokens <= budget.UsableInput {
		return nil
	}

	message := "model context preflight failed: estimated input is %d tokens, " +
		"usable input budget is %d tokens after reserving %d of %d context tokens; " +
		"start a fresh session or compact the conversation"

	return oops.In("contextwindow").
		Code("context_window_exceeded").
		With("context_tokens", budget.InputTokens).
		With("context_window", budget.ContextWindow).
		With("usable_input_tokens", budget.UsableInput).
		With("reserved_tokens", budget.TotalReserve()).
		Errorf(message, budget.InputTokens, budget.UsableInput, budget.TotalReserve(), budget.ContextWindow)
}

// OutputReserve returns the output token reserve for the given model/policy.
func OutputReserve(_ *model.Model, contextWindow int, policy config.ContextConfig) int {
	if policy.OutputReserveTokens > 0 {
		return policy.OutputReserveTokens
	}
	reserve := DefaultOutputReserve
	if contextWindow > 0 {
		reserve = min(reserve, max(1, contextWindow*ReservePercent/100))
	}

	return reserve
}

func nonNegativeOrDefault(value, fallback int) int {
	if value >= 0 {
		return value
	}

	return fallback
}

// BudgetFromUsage reconstructs budget diagnostics from usage breakdown fields.
func BudgetFromUsage(usage model.TokenUsage) Budget {
	return Budget{
		InputTokens:       usage.ContextTokens,
		ContextWindow:     usage.ContextWindow,
		UsableInput:       usage.Breakdown["usable_input"],
		OutputReserve:     usage.Breakdown["reserve_output"],
		ToolSchemaReserve: usage.Breakdown["reserve_tools"],
		ProviderReserve:   usage.Breakdown["reserve_provider"],
		SafetyMargin:      usage.Breakdown["reserve_safety"],
	}
}
