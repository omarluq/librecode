package assistant

import (
	"context"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/provider"
	"github.com/omarluq/librecode/internal/tool"
)

const (
	apiOpenAICompletions = "openai-completions"
	apiOpenAIResponses   = "openai-responses"
	apiAnthropicMessages = "anthropic-messages"
	jsonTypeKey          = "type"
	jsonContentKey       = "content"
	jsonPropertiesKey    = "properties"
	jsonPathKey          = "path"
	jsonQueryKey         = "query"
	jsonObjectType       = "object"
	jsonStringType       = "string"
	jsonSystemRole       = "system"
	jsonReadToolName     = "read"
	jsonBashToolName     = "bash"
	thinkingOff          = "off"
)

// ToolExecutor executes provider-requested tool calls through the assistant runtime.
type ToolExecutor func(context.Context, []ToolCall, func(StreamEvent)) ([]ToolEvent, error)

// CompletionRequest describes one assistant-owned model completion request.
type CompletionRequest struct {
	OnEvent           func(StreamEvent)                              `json:"-"`
	OnProviderObserve func(context.Context, *CompletionRequest, int) `json:"-"`
	OnProviderRequest llm.ProviderRequestHook                        `json:"-"`
	ToolRegistry      *tool.Registry                                 `json:"-"`
	ExecuteTools      ToolExecutor                                   `json:"-"`
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

// CompletionResult is an assistant-owned provider response plus model-visible side effects.
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
	IsError       bool   `json:"is_error,omitempty"`
}

// ToolCall is an assistant-local tool invocation requested by the model.
type ToolCall struct {
	Metadata      map[string]any `json:"metadata,omitempty"`
	Arguments     map[string]any `json:"arguments,omitempty"`
	ID            string         `json:"id"`
	Name          string         `json:"name"`
	ArgumentsJSON string         `json:"arguments_json,omitempty"`
}

// Completer talks to provider APIs through assistant-owned request/result types.
type Completer interface {
	Complete(ctx context.Context, request *CompletionRequest) (*CompletionResult, error)
}

// HTTPClient adapts the provider HTTP client to assistant-owned types.
type HTTPClient struct {
	provider *provider.HTTPCompletionClient
}

// NewHTTPClient creates an HTTP-backed provider client.
func NewHTTPClient() *HTTPClient {
	return &HTTPClient{provider: provider.NewHTTPCompletionClient()}
}

// Complete sends an assistant-owned completion request to the provider client.
func (client *HTTPClient) Complete(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	response, err := client.provider.Complete(ctx, providerRequestFromCompletionRequest(request))
	if err != nil {
		return nil, err
	}

	return completionResultFromLLMResponse(response), nil
}
