package assistant

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
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
	jsonPatternKey          = "pattern"
	jsonObjectType          = "object"
	jsonToolNameKey         = "name"
	jsonToolParamsKey       = "parameters"
	jsonCallIDKey           = "call_id"
	jsonOutputKey           = "output"
	jsonToolChoiceKey       = "tool_choice"
	jsonUserRole            = "user"
	functionToolType        = "function"
	functionCallType        = "function_call"
	functionCallOutputType  = "function_call_output"
	reasoningEffortKey      = "effort"
	thinkingOff             = "off"
	reasoningSummaryAuto    = "auto"
)

// CompletionRequest describes one model completion request.
type CompletionRequest struct {
	OnEvent       func(StreamEvent)        `json:"-"`
	SessionID     string                   `json:"session_id"`
	SystemPrompt  string                   `json:"system_prompt"`
	ThinkingLevel string                   `json:"thinking_level"`
	CWD           string                   `json:"cwd"`
	Auth          model.RequestAuth        `json:"auth"`
	Messages      []database.MessageEntity `json:"messages"`
	Model         model.Model              `json:"model"`
}

// CompletionResult is a provider response plus model-visible side effects.
type CompletionResult struct {
	Text       string      `json:"text"`
	Thinking   []string    `json:"thinking,omitempty"`
	ToolEvents []ToolEvent `json:"tool_events,omitempty"`
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
}

type providerResult struct {
	Text        string
	OutputItems []any
	Thinking    []string
	ToolCalls   []toolCall
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

func textCompletionResult(text string) *CompletionResult {
	return &CompletionResult{Text: strings.TrimSpace(text), Thinking: nil, ToolEvents: nil}
}
