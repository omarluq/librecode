package provider

import (
	"context"
	"net/http"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
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
	jsonTextKey             = "text"
	jsonInputKey            = "input"
	jsonThinkingKey         = "thinking"
	jsonDisplayKey          = "display"
	jsonUserRole            = "user"
	jsonAssistantRole       = "assistant"
	jsonToolRole            = "tool"
	jsonSystemRole          = "system"
	jsonMessagesKey         = "messages"
	jsonMessageType         = "message"
	jsonCommandKey          = "command"
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
	jsonFunctionKey         = "function"
	functionToolType        = jsonFunctionKey
	functionCallType        = "function_call"
	functionCallOutputType  = "function_call_output"
	anthropicToolUseType    = "tool_use"
	anthropicToolResultType = "tool_result"
	reasoningEffortKey      = "effort"
	thinkingOff             = "off"
	thinkingMinimal         = "minimal"
	thinkingLow             = "low"
	thinkingMedium          = "medium"
	thinkingHigh            = "high"
	thinkingXHigh           = "xhigh"
	thinkingDisplaySummary  = "summarized"
	reasoningSummaryAuto    = "auto"
	reasoningEffortNone     = "none"
	sseItemIDKey            = "item_id"
	sseOutputItemIDKey      = "output_item_id"
)

// CompletionRequest describes one provider-neutral generation request plus runtime callbacks.
type CompletionRequest struct {
	OnProviderObserve llm.ProviderObserver
	OnProviderRequest llm.ProviderRequestHook
	ExecuteTools      llm.ToolExecutor
	OnEvent           func(*llm.StreamChunk)
	Request           llm.Request
	ProviderAttempt   int
}

// Completer talks to provider APIs.
type Completer interface {
	Complete(ctx context.Context, request *CompletionRequest) (*llm.Response, error)
}

// ToolCall is a provider-returned or text-fallback local tool invocation.
type ToolCall struct {
	Arguments     map[string]any
	Metadata      map[string]any
	ID            string
	Name          string
	ArgumentsJSON string
	TextFallback  bool
}

// ToolEvent captures one tool result for provider follow-up messages.
type ToolEvent struct {
	Name          string
	ArgumentsJSON string
	DetailsJSON   string
	Result        string
	Error         string
	IsError       bool
}

type providerResult struct {
	Text        string
	OutputItems []any
	Thinking    []string
	ToolCalls   []ToolCall
	Usage       llm.Usage
}

// HTTPCompletionClient is a small provider client for built-in API families.
type HTTPCompletionClient struct {
	client *http.Client
}

// NewHTTPCompletionClient creates an HTTP-backed completion client.
func NewHTTPCompletionClient() *HTTPCompletionClient {
	return &HTTPCompletionClient{client: &http.Client{Timeout: 10 * time.Minute}}
}

// Generate sends a provider-neutral request without runtime callbacks.
func (client *HTTPCompletionClient) Generate(ctx context.Context, request *llm.Request) (*llm.Response, error) {
	completionRequest := CompletionRequest{
		OnProviderObserve: nil,
		OnProviderRequest: nil,
		ExecuteTools:      nil,
		OnEvent:           nil,
		Request:           emptyRequest(),
		ProviderAttempt:   0,
	}
	if request != nil {
		completionRequest.Request = *request
	}

	return client.Complete(ctx, &completionRequest)
}

// Complete sends a request to the selected provider.
func (client *HTTPCompletionClient) Complete(
	ctx context.Context,
	request *CompletionRequest,
) (*llm.Response, error) {
	if request == nil {
		return nil, oops.In("provider").
			Code("invalid_completion_request").
			Errorf("completion request is nil")
	}

	api := request.Request.Model.API
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
		return nil, oops.In("provider").
			Code("unsupported_provider_api").
			With("api", api).
			Errorf("provider api is not implemented")
	}
}

func emptyRequest() llm.Request {
	return llm.EmptyRequest()
}
