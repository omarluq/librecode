package provider

import (
	"context"
	"crypto/tls"
	"net/http"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
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
	jsonAllowIgnoredKey     = "allow_ignored"
	jsonIgnoreCaseKey       = "ignore_case"
	jsonPatternKey          = "pattern"
	jsonObjectType          = "object"
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
	jsonReasoningKey        = "reasoning"
	jsonToolsKey            = "tools"
	reasoningContentKey     = "reasoning.encrypted_content"
	jsonDisplayKey          = "display"
	jsonUserRole            = "user"
	jsonAssistantRole       = "assistant"
	jsonToolRole            = "tool"
	jsonSystemRole          = "system"
	jsonMessagesKey         = "messages"
	jsonMessageKey          = "message"
	jsonMessageType         = "message"
	jsonUsageKey            = "usage"
	jsonCommandKey          = "command"
	jsonReadToolName        = "read"
	jsonBashToolName        = "bash"
	jsonEditToolName        = "edit"
	jsonWriteToolName       = "write"
	jsonGrepToolName        = "grep"
	jsonFindToolName        = "find"
	jsonLSToolName          = "ls"
	jsonASTToolName         = "ast"
	jsonFetchToolName       = "fetch"
	anthropicReadToolName   = "Read"
	anthropicBashToolName   = "Bash"
	anthropicEditToolName   = "Edit"
	anthropicWriteToolName  = "Write"
	anthropicGrepToolName   = "Grep"
	anthropicFindToolName   = "Find"
	anthropicLSToolName     = "LS"
	jsonOldTextKey          = "old_text"
	jsonNewTextKey          = "new_text"
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
	thinkingMax             = "max"
	thinkingDisplaySummary  = "summarized"
	reasoningSummaryAuto    = "auto"
	reasoningEffortNone     = "none"
	statusCompleted         = "completed"
	finishReasonMaxTokens   = "max_tokens"
	sseItemIDKey            = "item_id"
	sseOutputItemIDKey      = "output_item_id"
	jsonStreamKey           = "stream"
	sseDoneData             = "[DONE]"
	thinkingEnabled         = "enabled"
	thinkingDisabled        = "disabled"
	anthropicErrorEvent     = "error"
	anthropicDeltaKey       = "delta"
	jsonChoicesKey          = "choices"
	jsonFinishReasonKey     = "finish_reason"
	jsonToolCallsKey        = "tool_calls"
	jsonIndexKey            = "index"
	jsonInputTokensKey      = "input_tokens"
	openAIStopReason        = "stop"
	openAIToolCallsReason   = jsonToolCallsKey
	anthropicStopReasonKey  = "stop_reason"
	anthropicRefusalReason  = "refusal"
	http2ProtoMajor         = 2
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

// ToolCall is a provider-returned local tool invocation.
type ToolCall struct {
	Metadata      map[string]any
	ArgumentsJSON string
	ID            string
	Name          string
	Arguments     tool.Arguments
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
	FinishReason llm.FinishReason
	Text         string
	OutputItems  []any
	Thinking     []string
	ToolCalls    []ToolCall
	Usage        llm.Usage
}

// HTTPCompletionClient is a small provider client for built-in API families.
type HTTPCompletionClient struct {
	client *http.Client
}

const providerHTTPTimeout = 10 * time.Minute

const (
	providerMaxIdleConns        = 100
	providerMaxIdleConnsPerHost = 10
	providerIdleConnTimeout     = 90 * time.Second
	providerTLSHandshakeTimeout = 10 * time.Second
)

// NewHTTPCompletionClient creates an HTTP-backed completion client.
func NewHTTPCompletionClient() *HTTPCompletionClient {
	transport := &http.Transport{
		Proxy:                 http.ProxyFromEnvironment,
		MaxIdleConns:          providerMaxIdleConns,
		MaxIdleConnsPerHost:   providerMaxIdleConnsPerHost,
		IdleConnTimeout:       providerIdleConnTimeout,
		TLSHandshakeTimeout:   providerTLSHandshakeTimeout,
		ExpectContinueTimeout: 1 * time.Second,
		ForceAttemptHTTP2:     true,
		TLSClientConfig: &tls.Config{
			MinVersion: tls.VersionTLS12,
			NextProtos: []string{"h2"},
		},
	}

	return &HTTPCompletionClient{
		client: &http.Client{
			Timeout:   providerHTTPTimeout,
			Transport: h2OnlyTransport{base: transport},
		},
	}
}

type h2OnlyTransport struct {
	base http.RoundTripper
}

func (transport h2OnlyTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	response, err := transport.base.RoundTrip(request)
	if err != nil {
		return nil, oops.In("provider").Code("provider_http2_roundtrip").Wrapf(err, "request provider response")
	}

	if response.ProtoMajor == http2ProtoMajor {
		return response, nil
	}

	closeBody(response.Body)

	return nil, oops.In("provider").
		Code("provider_http2_required").
		With("proto", response.Proto).
		Errorf("provider endpoint negotiated non-http2 protocol; HTTP/2 is required")
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
