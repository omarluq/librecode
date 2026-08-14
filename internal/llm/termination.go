package llm

import "strings"

const maxTerminationMetadataBytes = 64

// TerminationMetadata preserves bounded provider enums needed to disambiguate
// normalized finish reasons. It never contains response text or identifiers.
type TerminationMetadata struct {
	ProviderStatus       string `json:"provider_status,omitempty"`
	ProviderFinishReason string `json:"provider_finish_reason,omitempty"`
	IncompleteReason     string `json:"incomplete_reason,omitempty"`
}

// NewTerminationMetadata normalizes and bounds provider-owned enum values.
func NewTerminationMetadata(status, finish, incomplete string) TerminationMetadata {
	return TerminationMetadata{
		ProviderStatus:       boundedEnum(status),
		ProviderFinishReason: boundedEnum(finish),
		IncompleteReason:     boundedEnum(incomplete),
	}
}

func boundedEnum(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	if len(value) > maxTerminationMetadataBytes {
		value = value[:maxTerminationMetadataBytes]
	}

	return value
}
