package assistant

import (
	"encoding/json"
	"fmt"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/provider"
)

// computeToolSchemaTokens marshals API-specific tool declarations to JSON
// and estimates tokens from the resulting string. This is the uncached
// computation; callers should use Runtime.estimateToolSchemaTokens instead.
func computeToolSchemaTokens(request *CompletionRequest) int {
	if request == nil || request.DisableTools {
		return 0
	}

	definitions := llmToolDefinitionsFromRegistry(request.ToolRegistry, request.DisableTools)

	var tools []map[string]any

	switch request.Model.API {
	case apiOpenAICompletions:
		tools = provider.OpenAIChatToolsFromDefinitions(definitions)
	case apiAnthropicMessages:
		tools = provider.AnthropicToolsFromDefinitions(definitions, requestUsesAnthropicOAuth(request))
	default:
		tools = provider.ResponseToolsFromDefinitions(definitions)
	}

	if len(tools) == 0 {
		return 0
	}

	encoded, err := json.Marshal(tools)
	if err != nil {
		return contextwindow.EstimateTokens(fmt.Sprint(tools))
	}

	return contextwindow.EstimateTokens(string(encoded))
}
