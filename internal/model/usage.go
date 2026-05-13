package model

// TokenUsage tracks model context and request/response token counts.
type TokenUsage struct {
	ContextWindow int `json:"context_window,omitempty"`
	ContextTokens int `json:"context_tokens,omitempty"`
	InputTokens   int `json:"input_tokens,omitempty"`
	OutputTokens  int `json:"output_tokens,omitempty"`
}

// EmptyTokenUsage returns a zero-value token usage with explicit fields.
func EmptyTokenUsage() TokenUsage {
	return TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: 0, OutputTokens: 0}
}

// TotalTokens returns input plus output tokens reported for the turn.
func (usage TokenUsage) TotalTokens() int {
	return usage.InputTokens + usage.OutputTokens
}

// HasAny reports whether any usage field is populated.
func (usage TokenUsage) HasAny() bool {
	return usage.ContextWindow > 0 || usage.ContextTokens > 0 || usage.InputTokens > 0 || usage.OutputTokens > 0
}

// ContextPercent returns the context-window usage percentage, if known.
func (usage TokenUsage) ContextPercent() int {
	if usage.ContextWindow <= 0 || usage.ContextTokens <= 0 {
		return 0
	}
	percent := usage.ContextTokens * 100 / usage.ContextWindow
	if percent > 100 {
		return 100
	}

	return percent
}
