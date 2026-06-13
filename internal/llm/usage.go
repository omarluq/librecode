package llm

import "github.com/omarluq/librecode/internal/units"

// TokenContributor describes a large piece of model-facing context.
type TokenContributor struct {
	Label   string `json:"label"`
	Role    string `json:"role,omitempty"`
	Preview string `json:"preview,omitempty"`
	Tokens  int    `json:"tokens"`
	Chars   int    `json:"chars"`
}

// Usage tracks model context and request/response token counts.
type Usage struct {
	Breakdown       map[string]int     `json:"breakdown,omitempty"`
	TopContributors []TokenContributor `json:"top_contributors,omitempty"`
	ContextWindow   int                `json:"context_window,omitempty"`
	ContextTokens   int                `json:"context_tokens,omitempty"`
	InputTokens     int                `json:"input_tokens,omitempty"`
	OutputTokens    int                `json:"output_tokens,omitempty"`
}

// EmptyUsage returns explicit zero usage.
func EmptyUsage() Usage {
	return Usage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}
}

// TotalTokens returns input plus output tokens reported for the turn.
func (usage Usage) TotalTokens() int {
	return usage.InputTokens + usage.OutputTokens
}

// HasAny reports whether any usage field is populated.
func (usage Usage) HasAny() bool {
	return usage.ContextWindow > 0 || usage.ContextTokens > 0 || usage.InputTokens > 0 || usage.OutputTokens > 0 ||
		len(usage.Breakdown) > 0 || len(usage.TopContributors) > 0
}

// ContextPercent returns the context-window usage percentage, if known.
func (usage Usage) ContextPercent() int {
	if usage.ContextWindow <= 0 || usage.ContextTokens <= 0 {
		return 0
	}

	percent := usage.ContextTokens * units.PercentScale / usage.ContextWindow
	if percent > units.PercentScale {
		return units.PercentScale
	}

	return percent
}
