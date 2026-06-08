package assistant

import (
	"encoding/json"
	"fmt"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/config"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/provider"
)

const (
	defaultContextOutputReserve   = 16_384
	defaultContextSafetyMargin    = 8_192
	defaultContextProviderReserve = 2_048
	contextReservePercent         = 20
)

type contextBudget struct {
	InputTokens       int
	ContextWindow     int
	UsableInput       int
	OutputReserve     int
	ToolSchemaReserve int
	ProviderReserve   int
	SafetyMargin      int
}

func newContextBudget(
	usage model.TokenUsage,
	selectedModel *model.Model,
	policy config.ContextConfig,
	request *CompletionRequest,
) contextBudget {
	contextWindow := usage.ContextWindow
	if contextWindow <= 0 && selectedModel != nil {
		contextWindow = selectedModel.ContextWindow
	}
	budget := contextBudget{
		InputTokens:       usage.ContextTokens,
		ContextWindow:     contextWindow,
		UsableInput:       0,
		OutputReserve:     contextOutputReserve(selectedModel, contextWindow, policy),
		ToolSchemaReserve: estimateToolSchemaTokens(request),
		ProviderReserve:   nonNegativeOrDefault(policy.ProviderReserveTokens, defaultContextProviderReserve),
		SafetyMargin:      nonNegativeOrDefault(policy.SafetyMarginTokens, defaultContextSafetyMargin),
	}
	budget.UsableInput = max(contextWindow-budget.TotalReserve(), 0)

	return budget
}

func (budget contextBudget) TotalReserve() int {
	return budget.OutputReserve + budget.ToolSchemaReserve + budget.ProviderReserve + budget.SafetyMargin
}

func (budget contextBudget) UsageWithBudget(usage model.TokenUsage) model.TokenUsage {
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

func (budget contextBudget) Validate() error {
	if budget.ContextWindow <= 0 || budget.InputTokens <= budget.UsableInput {
		return nil
	}

	message := "model context preflight failed: estimated input is %d tokens, " +
		"usable input budget is %d tokens after reserving %d of %d context tokens; " +
		"start a fresh session or compact the conversation"

	return oops.In("assistant").
		Code("context_window_exceeded").
		With("context_tokens", budget.InputTokens).
		With("context_window", budget.ContextWindow).
		With("usable_input_tokens", budget.UsableInput).
		With("reserved_tokens", budget.TotalReserve()).
		Errorf(message, budget.InputTokens, budget.UsableInput, budget.TotalReserve(), budget.ContextWindow)
}

func contextOutputReserve(_ *model.Model, contextWindow int, policy config.ContextConfig) int {
	if policy.OutputReserveTokens > 0 {
		return policy.OutputReserveTokens
	}
	reserve := defaultContextOutputReserve
	if contextWindow > 0 {
		reserve = min(reserve, max(1, contextWindow*contextReservePercent/100))
	}

	return reserve
}

func estimateToolSchemaTokens(request *CompletionRequest) int {
	if request == nil {
		return 0
	}

	var tools []map[string]any
	switch request.Model.API {
	case apiOpenAICompletions:
		tools = provider.OpenAIChatTools(request)
	case apiAnthropicMessages:
		tools = provider.AnthropicTools(request)
	default:
		tools = provider.ResponseTools(request)
	}
	if len(tools) == 0 {
		return 0
	}
	encoded, err := json.Marshal(tools)
	if err != nil {
		return estimateTokens(fmt.Sprint(tools))
	}

	return estimateTokens(string(encoded))
}

func nonNegativeOrDefault(value, fallback int) int {
	if value >= 0 {
		return value
	}

	return fallback
}
