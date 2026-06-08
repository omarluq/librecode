package assistant

import (
	"context"
	"net/http"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	apiOpenAICompletions    = "openai-completions"
	apiOpenAIResponses      = "openai-responses"
	apiOpenAICodexResponses = "openai-codex-responses"
	apiAnthropicMessages    = "anthropic-messages"
	jsonModelKey            = "model"
	jsonContentKey          = "content"
	jsonRoleKey             = "role"
	jsonSummaryKey          = "summary"
	jsonTypeKey             = "type"
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
	jsonInputSchemaKey      = "input_schema"
	jsonArgumentsKey        = "arguments"
	jsonCallIDKey           = "call_id"
	jsonOutputKey           = "output"
	jsonOutputTokensKey     = "output_tokens"
	jsonToolChoiceKey       = "tool_choice"
	jsonInputTokensKey      = "input_tokens"
	jsonContextTokensKey    = "context_tokens"
	jsonContextWindowKey    = "context_window"
	jsonSessionIDKey        = "session_id"
	jsonTextKey             = "text"
	jsonInputKey            = "input"
	jsonThinkingKey         = "thinking"
	jsonDisplayKey          = "display"
	jsonUsageKey            = "usage"
	jsonUserRole            = "user"
	jsonAssistantRole       = "assistant"
	jsonToolRole            = "tool"
	jsonSystemRole          = "system"
	jsonMessagesKey         = "messages"
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
	anthropicReadToolName   = "Read"
	anthropicBashToolName   = "Bash"
	anthropicEditToolName   = "Edit"
	anthropicWriteToolName  = "Write"
	anthropicGrepToolName   = "Grep"
	anthropicFindToolName   = "Find"
	anthropicLSToolName     = "LS"
	jsonOldTextKey          = "oldText"
	jsonNewTextKey          = "newText"
	functionToolType        = "function"
	functionCallType        = "function_call"
	functionCallOutputType  = "function_call_output"
	anthropicToolUseType    = "tool_use"
	anthropicToolResultType = "tool_result"
	reasoningEffortKey      = "effort"
	thinkingOff             = "off"
	thinkingLow             = "low"
	thinkingHigh            = "high"
	thinkingXHigh           = "xhigh"
	thinkingDisplaySummary  = "summarized"
	reasoningSummaryAuto    = "auto"
)

// CompletionRequest describes one model completion request.
type CompletionRequest struct {
	OnEvent           func(StreamEvent)                              `json:"-"`
	OnProviderObserve func(context.Context, *CompletionRequest, int) `json:"-"`
	OnProviderRequest ProviderRequestHook                            `json:"-"`
	OnToolCall        func(context.Context, *ToolCallEvent) error    `json:"-"`
	OnToolResult      func(context.Context, *ToolEvent) error        `json:"-"`
	ToolRegistry      *tool.Registry                                 `json:"-"`
	SessionID         string                                         `json:"session_id"`
	SystemPrompt      string                                         `json:"system_prompt"`
	ThinkingLevel     string                                         `json:"thinking_level"`
	CWD               string                                         `json:"cwd"`
	Auth              model.RequestAuth                              `json:"auth"`
	Messages          []database.MessageEntity                       `json:"messages"`
	Usage             model.TokenUsage                               `json:"usage"`
	Model             model.Model                                    `json:"model"`
	ProviderAttempt   int                                            `json:"-"`
	DisableTools      bool                                           `json:"-"`
}

// CompletionResult is a provider response plus model-visible side effects.
type CompletionResult struct {
	Text       string           `json:"text"`
	Thinking   []string         `json:"thinking,omitempty"`
	ToolEvents []ToolEvent      `json:"tool_events,omitempty"`
	Usage      model.TokenUsage `json:"usage"`
}

// ToolCallEvent captures one requested tool call before execution.
type ToolCallEvent struct {
	Arguments     map[string]any `json:"arguments,omitempty"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	ArgumentsJSON string         `json:"arguments_json"`
}

// ToolEvent captures one tool call for persistence and TUI rendering.
type ToolEvent struct {
	Name          string `json:"name"`
	ArgumentsJSON string `json:"arguments_json"`
	DetailsJSON   string `json:"details_json,omitempty"`
	Result        string `json:"result"`
	Error         string `json:"error,omitempty"`
}

// CompletionClient talks to provider APIs.
type CompletionClient interface {
	Complete(ctx context.Context, request *CompletionRequest) (*CompletionResult, error)
}

type toolCall struct {
	Arguments     map[string]any
	ID            string
	Name          string
	ArgumentsJSON string
	TextFallback  bool
}

type providerResult struct {
	Text        string
	OutputItems []any
	Thinking    []string
	ToolCalls   []toolCall
	Usage       model.TokenUsage
}

// HTTPCompletionClient is a small provider client for built-in API families.
type HTTPCompletionClient struct {
	client *http.Client
}

// NewHTTPCompletionClient creates an HTTP-backed completion client.
func NewHTTPCompletionClient() *HTTPCompletionClient {
	return &HTTPCompletionClient{client: &http.Client{Timeout: 10 * time.Minute}}
}

// Complete sends a request to the selected provider.
func (client *HTTPCompletionClient) Complete(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	api := request.Model.API
	if api == "" {
		api = apiOpenAICompletions
	}
	switch api {
	case apiOpenAICompletions:
		return client.completeOpenAIChat(ctx, request)
	case apiOpenAIResponses:
		return client.completeOpenAIResponses(ctx, request)
	case apiOpenAICodexResponses:
		return client.completeOpenAICodex(ctx, request)
	case apiAnthropicMessages:
		return client.completeAnthropic(ctx, request)
	default:
		return nil, oops.In("assistant").
			Code("unsupported_provider_api").
			With("api", api).
			Errorf("provider api is not implemented")
	}
}
