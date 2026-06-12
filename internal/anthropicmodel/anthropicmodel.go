// Package anthropicmodel contains Anthropic model-family capability helpers.
package anthropicmodel

import "strings"

const (
	// Fable5 is Anthropic's Claude Fable 5 model ID.
	Fable5 = "claude-fable-5"
	// Mythos5 is Anthropic's Claude Mythos 5 model ID.
	Mythos5 = "claude-mythos-5"

	fable5Family   = "fable-5"
	mythos5Family  = "mythos-5"
	mythosPreview  = "mythos-preview"
	opus46Hyphen   = "opus-4-6"
	opus46Dot      = "opus-4.6"
	opus47Hyphen   = "opus-4-7"
	opus47Dot      = "opus-4.7"
	opus48Hyphen   = "opus-4-8"
	opus48Dot      = "opus-4.8"
	sonnet46Hyphen = "sonnet-4-6"
	sonnet46Dot    = "sonnet-4.6"
)

// RequiresAdaptiveThinking reports whether the model requires adaptive thinking
// and rejects explicit disabled-thinking payloads.
func RequiresAdaptiveThinking(modelID string) bool {
	normalizedModelID := strings.ToLower(modelID)

	return strings.Contains(normalizedModelID, fable5Family) ||
		strings.Contains(normalizedModelID, mythos5Family) ||
		strings.Contains(normalizedModelID, mythosPreview)
}

// SupportsAdaptiveThinking reports whether the model supports Anthropic's
// adaptive-thinking payload shape.
func SupportsAdaptiveThinking(modelID string) bool {
	normalizedModelID := strings.ToLower(modelID)

	return RequiresAdaptiveThinking(normalizedModelID) ||
		strings.Contains(normalizedModelID, opus46Hyphen) ||
		strings.Contains(normalizedModelID, opus46Dot) ||
		strings.Contains(normalizedModelID, opus47Hyphen) ||
		strings.Contains(normalizedModelID, opus47Dot) ||
		strings.Contains(normalizedModelID, opus48Hyphen) ||
		strings.Contains(normalizedModelID, opus48Dot) ||
		strings.Contains(normalizedModelID, sonnet46Hyphen) ||
		strings.Contains(normalizedModelID, sonnet46Dot)
}

// SupportsXHigh reports whether the model supports librecode's xhigh thinking
// level mapping.
func SupportsXHigh(modelID string) bool {
	normalizedModelID := strings.ToLower(modelID)

	return RequiresAdaptiveThinking(normalizedModelID) ||
		strings.Contains(normalizedModelID, opus47Hyphen) ||
		strings.Contains(normalizedModelID, opus47Dot) ||
		strings.Contains(normalizedModelID, opus48Hyphen) ||
		strings.Contains(normalizedModelID, opus48Dot)
}
