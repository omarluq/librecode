package model

import "github.com/omarluq/librecode/internal/units"

// TokenContributor describes a large piece of model-facing context.
type TokenContributor struct {
	Label   string `json:"label"`
	Role    string `json:"role,omitempty"`
	Preview string `json:"preview,omitempty"`
	Tokens  int    `json:"tokens"`
	Chars   int    `json:"chars"`
}

// TokenUsage tracks model context and request/response token counts.
// InputTokens and OutputTokens are cumulative across provider rounds in one completion.
// ContextTokens is the input size of the latest provider request.
type TokenUsage struct {
	Breakdown       map[string]int     `json:"breakdown,omitempty"`
	TopContributors []TokenContributor `json:"top_contributors,omitempty"`
	ContextWindow   int                `json:"context_window,omitempty"`
	ContextTokens   int                `json:"context_tokens,omitempty"`
	InputTokens     int                `json:"input_tokens,omitempty"`
	OutputTokens    int                `json:"output_tokens,omitempty"`
	reported        bool
}

// EmptyTokenUsage returns a zero-value token usage with explicit fields.
func EmptyTokenUsage() TokenUsage {
	return TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
		reported:        false,
	}
}

// WithReported marks usage as explicitly reported by a provider, including a zero-token report.
func (usage TokenUsage) WithReported() TokenUsage {
	usage.reported = true

	return usage
}

// Reported reports whether the provider supplied a usage object.
func (usage TokenUsage) Reported() bool {
	return usage.reported
}

// TotalTokens returns input plus output tokens reported for the turn.
func (usage TokenUsage) TotalTokens() int {
	return usage.InputTokens + usage.OutputTokens
}

// HasAny reports whether any usage field is populated.
func (usage TokenUsage) HasAny() bool {
	return usage.reported || usage.ContextWindow > 0 || usage.ContextTokens > 0 || usage.InputTokens > 0 ||
		usage.OutputTokens > 0 || len(usage.Breakdown) > 0 || len(usage.TopContributors) > 0
}

// ContextPercent returns the context-window usage percentage, if known.
func (usage TokenUsage) ContextPercent() int {
	if usage.ContextWindow <= 0 || usage.ContextTokens <= 0 {
		return 0
	}

	return units.PercentOf(usage.ContextTokens, usage.ContextWindow)
}
