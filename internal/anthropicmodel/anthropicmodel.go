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

	return hasFamily(normalizedModelID, fable5Family) ||
		hasFamily(normalizedModelID, mythos5Family) ||
		hasFamily(normalizedModelID, mythosPreview)
}

// SupportsAdaptiveThinking reports whether the model supports Anthropic's
// adaptive-thinking payload shape.
func SupportsAdaptiveThinking(modelID string) bool {
	normalizedModelID := strings.ToLower(modelID)

	return RequiresAdaptiveThinking(normalizedModelID) ||
		hasFamily(normalizedModelID, opus46Hyphen) ||
		hasFamily(normalizedModelID, opus46Dot) ||
		hasFamily(normalizedModelID, opus47Hyphen) ||
		hasFamily(normalizedModelID, opus47Dot) ||
		hasFamily(normalizedModelID, opus48Hyphen) ||
		hasFamily(normalizedModelID, opus48Dot) ||
		hasFamily(normalizedModelID, sonnet46Hyphen) ||
		hasFamily(normalizedModelID, sonnet46Dot)
}

// SupportsXHigh reports whether the model supports librecode's xhigh thinking
// level mapping.
func SupportsXHigh(modelID string) bool {
	normalizedModelID := strings.ToLower(modelID)

	return RequiresAdaptiveThinking(normalizedModelID) ||
		hasFamily(normalizedModelID, opus47Hyphen) ||
		hasFamily(normalizedModelID, opus47Dot) ||
		hasFamily(normalizedModelID, opus48Hyphen) ||
		hasFamily(normalizedModelID, opus48Dot)
}

func hasFamily(modelID, family string) bool {
	for offset := 0; ; {
		index := strings.Index(modelID[offset:], family)
		if index == -1 {
			return false
		}

		start := offset + index

		end := start + len(family)
		if isBoundary(modelID, start-1) && isBoundary(modelID, end) {
			return true
		}

		offset = end
	}
}

func isBoundary(value string, index int) bool {
	if index < 0 || index >= len(value) {
		return true
	}

	switch value[index] {
	case '-', '.', '_':
		return true
	default:
		return false
	}
}
