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

// UsageProvenance identifies how context usage was calculated.
type UsageProvenance string

const (
	// UsageProviderReported identifies usage reported directly by a provider.
	UsageProviderReported UsageProvenance = "provider_reported"
	// UsageProviderAnchorEstimate identifies usage based on a provider anchor plus local estimates.
	UsageProviderAnchorEstimate UsageProvenance = "provider_anchor_plus_estimate"
	// UsageLocalEstimate identifies usage estimated entirely locally.
	UsageLocalEstimate UsageProvenance = "local_estimate"
)

// Usage tracks model context and request/response token counts.
// InputTokens and OutputTokens are cumulative across provider rounds in one completion.
// ContextTokens is the input size of the latest provider request.
type Usage struct {
	Breakdown       map[string]int     `json:"breakdown,omitempty"`
	Provenance      UsageProvenance    `json:"provenance,omitempty"`
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
		Provenance:      "",
	}
}

// WithReported marks usage as explicitly reported by a provider, including a zero-token report.
func (usage *Usage) WithReported() Usage {
	reported := *usage
	reported.Provenance = UsageProviderReported

	return reported
}

// Reported reports whether the provider supplied a usage object.
func (usage *Usage) Reported() bool {
	return usage.Provenance == UsageProviderReported
}

// TotalTokens returns input plus output tokens reported for the turn.
func (usage *Usage) TotalTokens() int {
	return usage.InputTokens + usage.OutputTokens
}

// HasAny reports whether any usage field is populated.
func (usage *Usage) HasAny() bool {
	return usage.Reported() || usage.ContextWindow > 0 || usage.ContextTokens > 0 || usage.InputTokens > 0 ||
		usage.OutputTokens > 0 || len(usage.Breakdown) > 0 || len(usage.TopContributors) > 0
}

// ContextPercent returns the context-window usage percentage, if known.
func (usage *Usage) ContextPercent() int {
	if usage.ContextWindow <= 0 || usage.ContextTokens <= 0 {
		return 0
	}

	return units.PercentOf(usage.ContextTokens, usage.ContextWindow)
}
