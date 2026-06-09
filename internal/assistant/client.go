package assistant

import "github.com/omarluq/librecode/internal/provider"

const (
	apiOpenAICompletions = "openai-completions"
	apiOpenAIResponses   = "openai-responses"
	apiAnthropicMessages = "anthropic-messages"
	jsonTypeKey          = "type"
	jsonModelKey         = "model"
	jsonRoleKey          = "role"
	jsonContentKey       = "content"
	jsonSummaryKey       = "summary"
	jsonPropertiesKey    = "properties"
	jsonPathKey          = "path"
	jsonQueryKey         = "query"
	jsonObjectType       = "object"
	jsonStringType       = "string"
	jsonToolNameKey      = "name"
	jsonOutputTokensKey  = "output_tokens"
	jsonContextTokensKey = "context_tokens"
	jsonContextWindowKey = "context_window"
	jsonSessionIDKey     = "session_id"
	jsonTextKey          = "text"
	jsonUsageKey         = "usage"
	jsonSystemRole       = "system"
	jsonBreakdownKey     = "breakdown"
	jsonReadToolName     = "read"
	jsonBashToolName     = "bash"
	jsonInputTokensKey   = "input_tokens"
	thinkingOff          = "off"
)

// CompletionRequest aliases the provider completion request during the provider package extraction.
type CompletionRequest = provider.CompletionRequest

// CompletionResult aliases the provider completion result during the provider package extraction.
type CompletionResult = provider.CompletionResult

// ToolCall aliases provider tool calls during the provider package extraction.
type ToolCall = provider.ToolCall

// ToolCallEvent aliases the provider tool-call event during the provider package extraction.
type ToolCallEvent = provider.ToolCallEvent

// ToolEvent aliases the provider tool event during the provider package extraction.
type ToolEvent = provider.ToolEvent

// ProviderStreamEvent aliases provider stream events for tests and adapters.
type ProviderStreamEvent = provider.StreamEvent

// CompletionClient aliases the provider completion client interface.
type CompletionClient = provider.CompletionClient

// HTTPCompletionClient aliases the provider HTTP implementation.
type HTTPCompletionClient = provider.HTTPCompletionClient

// NewHTTPCompletionClient creates an HTTP-backed provider client.
func NewHTTPCompletionClient() *HTTPCompletionClient {
	return provider.NewHTTPCompletionClient()
}

const (
	// ProviderStreamEventKindTextDelta mirrors provider text delta events for test clients.
	ProviderStreamEventKindTextDelta = provider.StreamEventTextDelta
	// ProviderStreamEventKindToolResult mirrors provider tool result events for test clients.
	ProviderStreamEventKindToolResult = provider.StreamEventToolResult
)
