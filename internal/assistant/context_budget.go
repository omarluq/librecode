package assistant

import (
	"encoding/json"
	"fmt"

	"github.com/omarluq/librecode/internal/contextwindow"
	"github.com/omarluq/librecode/internal/provider"
)

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
		return contextwindow.EstimateTokens(fmt.Sprint(tools))
	}

	return contextwindow.EstimateTokens(string(encoded))
}
