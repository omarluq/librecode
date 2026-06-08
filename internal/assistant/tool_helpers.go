package assistant

import "github.com/omarluq/librecode/internal/provider"

func encodeToolArguments(arguments map[string]any) string {
	return provider.EncodeToolArguments(arguments)
}
