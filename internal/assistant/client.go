package assistant

import "github.com/omarluq/librecode/internal/provider"

const (
	apiOpenAICompletions    = "openai-completions"
	apiOpenAIResponses      = "openai-responses"
	apiOpenAICodexResponses = "openai-codex-responses"
	apiAnthropicMessages    = "anthropic-messages"
	jsonTypeKey             = "type"
	jsonModelKey            = "model"
	jsonRoleKey             = "role"
	jsonContentKey          = "content"
	jsonSummaryKey          = "summary"
	jsonDescriptionKey      = "description"
	jsonPropertiesKey       = "properties"
	jsonRequiredKey         = "required"
	jsonPathKey             = "path"
	jsonLimitKey            = "limit"
	jsonQueryKey            = "query"
	jsonAllowIgnoredKey     = "allowIgnored"
	jsonPatternKey          = "pattern"
	jsonObjectType          = "object"
	jsonStringType          = "string"
	jsonToolNameKey         = "name"
	jsonToolParamsKey       = "parameters"
	jsonInputKey            = "input"
	jsonOutputKey           = "output"
	jsonOutputTokensKey     = "output_tokens"
	jsonContextTokensKey    = "context_tokens"
	jsonContextWindowKey    = "context_window"
	jsonSessionIDKey        = "session_id"
	jsonTextKey             = "text"
	jsonDisplayKey          = "display"
	jsonThinkingKey         = "thinking"
	jsonUsageKey            = "usage"
	jsonUserRole            = "user"
	jsonAssistantRole       = "assistant"
	jsonToolRole            = "tool"
	jsonSystemRole          = "system"
	jsonCommandKey          = "command"
	jsonBreakdownKey        = "breakdown"
	jsonReadToolName        = "read"
	jsonBashToolName        = "bash"
	jsonEditToolName        = "edit"
	jsonWriteToolName       = "write"
	jsonGrepToolName        = "grep"
	jsonFindToolName        = "find"
	jsonLSToolName          = "ls"
	jsonASTToolName         = "ast"
	jsonOldTextKey          = "oldText"
	jsonNewTextKey          = "newText"
	jsonInputTokensKey      = "input_tokens"
	thinkingDisplaySummary  = "summarized"
	thinkingOff             = "off"
	functionToolType        = "function"
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
