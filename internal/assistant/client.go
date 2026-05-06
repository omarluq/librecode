package assistant

import (
	"bufio"
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
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
	jsonPatternKey          = "pattern"
	jsonObjectType          = "object"
	jsonToolNameKey         = "name"
	jsonToolParamsKey       = "parameters"
	jsonCallIDKey           = "call_id"
	jsonOutputKey           = "output"
	jsonToolChoiceKey       = "tool_choice"
	jsonUserRole            = "user"
	functionToolType        = "function"
	functionCallOutputType  = "function_call_output"
	reasoningEffortKey      = "effort"
	thinkingOff             = "off"
	reasoningSummaryAuto    = "auto"
	maxToolIterations       = 8
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

func (client *HTTPCompletionClient) completeOpenAIChat(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	payload := map[string]any{
		jsonModelKey:  request.Model.ID,
		"messages":    openAIChatMessages(request),
		"stream":      false,
		"temperature": 0.2,
	}
	if request.Model.Reasoning && request.ThinkingLevel != "" && request.ThinkingLevel != thinkingOff {
		payload["reasoning_effort"] = request.ThinkingLevel
	}
	endpoint := joinEndpoint(request.Model.BaseURL, "/chat/completions")
	content, err := client.postJSON(ctx, endpoint, openAIHeaders(request), payload)
	if err != nil {
		return nil, err
	}
	var response struct {
		Error   providerError `json:"error"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, oops.In("assistant").Code("openai_chat_decode").Wrapf(err, "decode chat response")
	}
	if response.Error.Message != "" {
		return nil, providerErrorToOops("openai_chat_error", &response.Error)
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return nil, oops.In("assistant").Code("openai_chat_empty").Errorf("provider returned an empty response")
	}

	return textCompletionResult(response.Choices[0].Message.Content), nil
}

func (client *HTTPCompletionClient) completeOpenAIResponses(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	input := openAIResponseInput(request.Messages)
	endpoint := joinEndpoint(request.Model.BaseURL, "/responses")

	return client.completeResponsesLoop(ctx, request, endpoint, openAIHeaders(request), input, false)
}

func (client *HTTPCompletionClient) completeOpenAICodex(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	input := openAIResponseInput(request.Messages)
	endpoint := joinEndpoint(request.Model.BaseURL, "/codex/responses")

	return client.completeResponsesLoop(ctx, request, endpoint, codexHeaders(request), input, true)
}

func (client *HTTPCompletionClient) completeAnthropic(
	ctx context.Context,
	request *CompletionRequest,
) (*CompletionResult, error) {
	payload := map[string]any{
		jsonModelKey:  request.Model.ID,
		"max_tokens":  minPositive(request.Model.MaxTokens, 4096),
		"system":      request.SystemPrompt,
		"messages":    anthropicMessages(request.Messages),
		"temperature": 0.2,
	}
	endpoint := joinEndpoint(request.Model.BaseURL, "/v1/messages")
	content, err := client.postJSON(ctx, endpoint, anthropicHeaders(request), payload)
	if err != nil {
		return nil, err
	}
	var response struct {
		Error   providerError `json:"error"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, oops.In("assistant").Code("anthropic_decode").Wrapf(err, "decode anthropic response")
	}
	if response.Error.Message != "" {
		return nil, providerErrorToOops("anthropic_error", &response.Error)
	}
	parts := make([]string, 0, len(response.Content))
	for _, block := range response.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return nil, oops.In("assistant").Code("anthropic_empty").Errorf("provider returned an empty response")
	}

	return textCompletionResult(text), nil
}

func textCompletionResult(text string) *CompletionResult {
	return &CompletionResult{Text: strings.TrimSpace(text), Thinking: nil, ToolEvents: nil}
}

func (client *HTTPCompletionClient) completeResponsesLoop(
	ctx context.Context,
	request *CompletionRequest,
	endpoint string,
	headers map[string]string,
	input []any,
	stream bool,
) (*CompletionResult, error) {
	result := &CompletionResult{Text: "", Thinking: nil, ToolEvents: nil}
	for iteration := 0; iteration < maxToolIterations; iteration++ {
		payload := responsesPayload(request, input, stream)
		providerResult, err := client.requestResponses(ctx, endpoint, headers, payload, stream, request.OnEvent)
		if err != nil {
			return nil, err
		}
		result.Thinking = append(result.Thinking, providerResult.Thinking...)
		if len(providerResult.ToolCalls) == 0 {
			if strings.TrimSpace(providerResult.Text) == "" {
				return nil, oops.In("assistant").Code("responses_empty").Errorf("provider returned an empty response")
			}
			result.Text = strings.TrimSpace(providerResult.Text)
			return result, nil
		}
		input = append(input, providerResult.OutputItems...)
		outputs, events := executeToolCalls(ctx, request.CWD, providerResult.ToolCalls, request.OnEvent)
		result.ToolEvents = append(result.ToolEvents, events...)
		input = append(input, outputs...)
	}

	return client.finalResponseWithoutTools(ctx, request, endpoint, headers, input, stream, result)
}

func responsesPayload(request *CompletionRequest, input []any, stream bool) map[string]any {
	payload := responsesBasePayload(request, input, stream)
	payload["tools"] = responseTools()
	payload[jsonToolChoiceKey] = "auto"
	payload["parallel_tool_calls"] = true

	return payload
}

func responsesBasePayload(request *CompletionRequest, input []any, stream bool) map[string]any {
	payload := map[string]any{
		jsonModelKey: request.Model.ID,
		"store":      false,
		"stream":     stream,
		"input":      input,
	}
	if request.SystemPrompt != "" {
		payload["instructions"] = request.SystemPrompt
	}
	if stream {
		payload["text"] = map[string]string{"verbosity": "low"}
		payload["include"] = []string{"reasoning.encrypted_content"}
		payload["prompt_cache_key"] = request.SessionID
	}
	if request.Model.Reasoning && request.ThinkingLevel != "" && request.ThinkingLevel != thinkingOff {
		payload["reasoning"] = map[string]any{
			reasoningEffortKey: request.ThinkingLevel,
			jsonSummaryKey:     reasoningSummaryAuto,
		}
	} else if stream {
		payload["reasoning"] = codexReasoning(request)
	}

	return payload
}

func (client *HTTPCompletionClient) finalResponseWithoutTools(
	ctx context.Context,
	request *CompletionRequest,
	endpoint string,
	headers map[string]string,
	input []any,
	stream bool,
	partial *CompletionResult,
) (*CompletionResult, error) {
	input = append(input, map[string]any{
		jsonRoleKey:    jsonUserRole,
		jsonContentKey: "Tool budget reached. Use the tool results above and answer without more tool calls.",
	})
	payload := responsesBasePayload(request, input, stream)
	providerResult, err := client.requestResponses(ctx, endpoint, headers, payload, stream, request.OnEvent)
	if err != nil {
		return nil, err
	}
	partial.Thinking = append(partial.Thinking, providerResult.Thinking...)
	if strings.TrimSpace(providerResult.Text) == "" {
		return nil, oops.
			In("assistant").
			Code("tool_loop_limit").
			With("iterations", maxToolIterations).
			Errorf("model kept requesting tools and did not produce a final answer")
	}
	partial.Text = strings.TrimSpace(providerResult.Text)

	return partial, nil
}

func (client *HTTPCompletionClient) requestResponses(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
	stream bool,
	onEvent func(StreamEvent),
) (*providerResult, error) {
	httpRequest, err := jsonRequest(ctx, endpoint, headers, payload)
	if err != nil {
		return nil, err
	}
	response, err := client.client.Do(httpRequest)
	if err != nil {
		return nil, oops.In("assistant").Code("responses_http").Wrapf(err, "request provider response")
	}
	defer closeBody(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		content, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return nil, oops.In("assistant").Code("responses_error_read").Wrapf(readErr, "read provider error")
		}

		return nil, providerStatusError("responses_status", response.StatusCode, content)
	}
	if stream {
		return parseSSEResult(response.Body, onEvent)
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, oops.In("assistant").Code("responses_read").Wrapf(err, "read provider response")
	}

	return parseOpenAIResponseResult(content)
}

func executeToolCalls(
	ctx context.Context,
	cwd string,
	calls []toolCall,
	onEvent func(StreamEvent),
) ([]any, []ToolEvent) {
	registry := tool.NewRegistry(cwd)
	outputs := make([]any, 0, len(calls))
	events := make([]ToolEvent, 0, len(calls))
	for _, call := range calls {
		emitStreamEvent(onEvent, StreamEvent{ToolEvent: nil, Kind: StreamEventToolStart, Text: call.Name})
		result, err := registry.Execute(ctx, call.Name, call.Arguments)
		resultText := result.Text()
		detailsJSON := encodeToolDetails(result.Details)
		event := ToolEvent{
			Name:          call.Name,
			ArgumentsJSON: call.ArgumentsJSON,
			DetailsJSON:   detailsJSON,
			Result:        resultText,
			Error:         "",
		}
		if err != nil {
			event.Error = err.Error()
			resultText = err.Error()
		}
		if strings.TrimSpace(resultText) == "" {
			resultText = "(tool returned no text output)"
		}
		event.Result = resultText
		events = append(events, event)
		emitStreamEvent(onEvent, StreamEvent{ToolEvent: &event, Kind: StreamEventToolResult, Text: ""})
		outputs = append(outputs, map[string]any{
			jsonTypeKey:   functionCallOutputType,
			jsonCallIDKey: call.ID,
			jsonOutputKey: toolOutputText(resultText, detailsJSON),
		})
	}

	return outputs, events
}

func emitStreamEvent(onEvent func(StreamEvent), event StreamEvent) {
	if onEvent != nil {
		onEvent(event)
	}
}

func encodeToolDetails(details map[string]any) string {
	if len(details) == 0 {
		return ""
	}
	encoded, err := json.Marshal(details)
	if err != nil {
		return ""
	}

	return string(encoded)
}

func toolOutputText(resultText, detailsJSON string) string {
	if strings.TrimSpace(detailsJSON) == "" {
		return resultText
	}
	trimmedResult := strings.TrimSpace(resultText)
	if trimmedResult == "" {
		return "details:\n" + detailsJSON
	}

	return trimmedResult + "\ndetails:\n" + detailsJSON
}

func (client *HTTPCompletionClient) postJSON(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
) ([]byte, error) {
	request, err := jsonRequest(ctx, endpoint, headers, payload)
	if err != nil {
		return nil, err
	}
	response, err := client.client.Do(request)
	if err != nil {
		return nil, oops.In("assistant").Code("provider_http").Wrapf(err, "request provider response")
	}
	defer closeBody(response.Body)
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, oops.In("assistant").Code("provider_read").Wrapf(err, "read provider response")
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, providerStatusError("provider_status", response.StatusCode, content)
	}

	return content, nil
}

func closeBody(body io.Closer) {
	if err := body.Close(); err != nil {
		return
	}
}

func jsonRequest(
	ctx context.Context,
	endpoint string,
	headers map[string]string,
	payload map[string]any,
) (*http.Request, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, oops.In("assistant").Code("provider_payload").Wrapf(err, "encode provider payload")
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, oops.In("assistant").Code("provider_request").Wrapf(err, "create provider request")
	}
	request.Header.Set("Content-Type", "application/json")
	for key, value := range headers {
		request.Header.Set(key, value)
	}

	return request, nil
}

func openAIHeaders(request *CompletionRequest) map[string]string {
	headers := cloneHeaders(request.Auth.Headers)
	headers["Authorization"] = "Bearer " + request.Auth.APIKey

	return headers
}

func codexHeaders(request *CompletionRequest) map[string]string {
	headers := openAIHeaders(request)
	accountID := request.Auth.Headers["chatgpt-account-id"]
	if accountID == "" {
		accountID = accountIDFromToken(request.Auth.APIKey)
	}
	headers["chatgpt-account-id"] = accountID
	headers["originator"] = "librecode"
	headers["User-Agent"] = "librecode"
	headers["OpenAI-Beta"] = "responses=experimental"
	headers["Accept"] = "text/event-stream"

	return headers
}

func anthropicHeaders(request *CompletionRequest) map[string]string {
	headers := cloneHeaders(request.Auth.Headers)
	headers["x-api-key"] = request.Auth.APIKey
	headers["anthropic-version"] = "2023-06-01"

	return headers
}

func openAIChatMessages(request *CompletionRequest) []map[string]string {
	messages := []map[string]string{}
	if request.SystemPrompt != "" {
		messages = append(messages, map[string]string{jsonRoleKey: "system", jsonContentKey: request.SystemPrompt})
	}
	for _, message := range request.Messages {
		role, ok := openAIRole(message.Role)
		if !ok || message.Content == "" {
			continue
		}
		messages = append(messages, map[string]string{jsonRoleKey: role, jsonContentKey: message.Content})
	}

	return messages
}

func openAIResponseInput(messages []database.MessageEntity) []any {
	input := []any{}
	for _, message := range messages {
		role, ok := openAIRole(message.Role)
		if !ok || message.Content == "" {
			continue
		}
		input = append(input, map[string]any{jsonRoleKey: role, jsonContentKey: message.Content})
	}

	return input
}

func anthropicMessages(messages []database.MessageEntity) []map[string]string {
	output := []map[string]string{}
	for _, message := range messages {
		role, ok := anthropicRole(message.Role)
		if !ok || message.Content == "" {
			continue
		}
		output = append(output, map[string]string{jsonRoleKey: role, jsonContentKey: message.Content})
	}

	return output
}

func responseTools() []map[string]any {
	definitions := tool.AllDefinitions()
	tools := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, map[string]any{
			jsonTypeKey:        functionToolType,
			jsonToolNameKey:    string(definition.Name),
			jsonDescriptionKey: definition.Description,
			jsonToolParamsKey:  toolParameterSchema(definition.Name),
			"strict":           false,
		})
	}

	return tools
}

func toolParameterSchema(name tool.Name) map[string]any {
	var schema map[string]any
	switch name {
	case tool.NameRead:
		schema = readToolSchema()
	case tool.NameBash:
		schema = bashToolSchema()
	case tool.NameEdit:
		schema = editToolSchema()
	case tool.NameWrite:
		schema = writeToolSchema()
	case tool.NameGrep:
		schema = grepToolSchema()
	case tool.NameFind:
		schema = findToolSchema()
	case tool.NameLS:
		schema = lsToolSchema()
	}
	if schema == nil {
		return map[string]any{jsonTypeKey: jsonObjectType, "additionalProperties": true}
	}
	schema["additionalProperties"] = false

	return schema
}

func readToolSchema() map[string]any {
	return map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			jsonPathKey:  stringSchema("Path to the file to read, relative to the current workspace or absolute."),
			"offset":     integerSchema("Optional 1-indexed line number to start reading from."),
			jsonLimitKey: integerSchema("Optional maximum number of lines to return."),
			"allowIgnored": booleanSchema(
				"Set true only when an ignored file is explicitly needed despite .gitignore/default ignores.",
			),
		},
		jsonRequiredKey: []string{jsonPathKey},
	}
}

func bashToolSchema() map[string]any {
	return map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			"command": stringSchema("Bash command to execute in the current workspace."),
			"timeout": numberSchema("Optional timeout in seconds."),
		},
		jsonRequiredKey: []string{"command"},
	}
}

func editToolSchema() map[string]any {
	return map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			jsonPathKey: stringSchema("Path to the file to edit, relative to the current workspace or absolute."),
			"edits":     editItemsSchema(),
		},
		jsonRequiredKey: []string{jsonPathKey, "edits"},
	}
}

func editItemsSchema() map[string]any {
	return map[string]any{
		jsonTypeKey: "array",
		"items": map[string]any{
			jsonTypeKey: jsonObjectType,
			jsonPropertiesKey: map[string]any{
				"oldText": stringSchema(
					"Exact text to replace. Must match a unique, non-overlapping region.",
				),
				"newText": stringSchema("Replacement text."),
			},
			jsonRequiredKey: []string{"oldText", "newText"},
		},
	}
}

func writeToolSchema() map[string]any {
	return map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			jsonPathKey: stringSchema(
				"Path to create or overwrite, relative to the current workspace or absolute.",
			),
			"content": stringSchema("Complete file content to write."),
		},
		jsonRequiredKey: []string{jsonPathKey, "content"},
	}
}

func grepToolSchema() map[string]any {
	return map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			jsonPatternKey: stringSchema("Regular expression or literal string to search for."),
			jsonPathKey:    stringSchema("Optional file or directory to search under."),
			"glob":         stringSchema("Optional glob filter such as **/*.go."),
			jsonLimitKey:   integerSchema("Optional maximum number of matches."),
			"context":      integerSchema("Optional number of context lines around each match."),
			"ignoreCase":   booleanSchema("Whether to match case-insensitively."),
			"literal":      booleanSchema("Whether pattern should be treated as literal text."),
		},
		jsonRequiredKey: []string{jsonPatternKey},
	}
}

func findToolSchema() map[string]any {
	return map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			jsonPatternKey: stringSchema("Glob pattern for file paths, such as **/*.go."),
			jsonPathKey:    stringSchema("Optional directory to search under."),
			jsonLimitKey:   integerSchema("Optional maximum number of paths."),
		},
		jsonRequiredKey: []string{jsonPatternKey},
	}
}

func lsToolSchema() map[string]any {
	return map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			jsonPathKey:  stringSchema("Optional directory to list."),
			jsonLimitKey: integerSchema("Optional maximum number of entries."),
		},
	}
}

func stringSchema(description string) map[string]any {
	return map[string]any{jsonTypeKey: "string", jsonDescriptionKey: description}
}

func integerSchema(description string) map[string]any {
	return map[string]any{jsonTypeKey: "integer", jsonDescriptionKey: description}
}

func numberSchema(description string) map[string]any {
	return map[string]any{jsonTypeKey: "number", jsonDescriptionKey: description}
}

func booleanSchema(description string) map[string]any {
	return map[string]any{jsonTypeKey: "boolean", jsonDescriptionKey: description}
}

func openAIRole(role database.Role) (string, bool) {
	switch role {
	case database.RoleUser:
		return jsonUserRole, true
	case database.RoleAssistant:
		return "assistant", true
	case database.RoleToolResult,
		database.RoleThinking,
		database.RoleCustom,
		database.RoleBashExecution,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return "", false
	}

	return "", false
}

func anthropicRole(role database.Role) (string, bool) {
	switch role {
	case database.RoleUser:
		return jsonUserRole, true
	case database.RoleAssistant:
		return "assistant", true
	case database.RoleToolResult,
		database.RoleThinking,
		database.RoleCustom,
		database.RoleBashExecution,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return "", false
	}

	return "", false
}

func parseOpenAIResponseResult(content []byte) (*providerResult, error) {
	var response map[string]any
	if err := json.Unmarshal(content, &response); err != nil {
		return nil, oops.In("assistant").Code("openai_response_decode").Wrapf(err, "decode response")
	}
	if errorValue, ok := response["error"]; ok {
		message := errorMessage(errorValue)
		if message != "" {
			return nil, oops.In("assistant").Code("openai_response_error").Errorf("%s", message)
		}
	}

	return providerResultFromResponse(response), nil
}

type sseAccumulator struct {
	itemByID      map[string]map[string]any
	finalResponse map[string]any
	parts         []string
	items         []any
}

func newSSEAccumulator() *sseAccumulator {
	return &sseAccumulator{
		itemByID:      map[string]map[string]any{},
		finalResponse: nil,
		parts:         []string{},
		items:         []any{},
	}
}

func (accumulator *sseAccumulator) add(event map[string]any, onEvent func(StreamEvent)) {
	if response, ok := event["response"].(map[string]any); ok {
		accumulator.finalResponse = response
	}
	if text, delta := textFromSSEEvent(event); delta && text != "" {
		accumulator.parts = append(accumulator.parts, text)
		emitStreamEvent(onEvent, StreamEvent{ToolEvent: nil, Kind: StreamEventTextDelta, Text: text})
	}
	if item, ok := event["item"].(map[string]any); ok {
		accumulator.addItem(item)
	}
	if arguments, ok := event["arguments"].(string); ok {
		accumulator.addArguments(event, arguments)
	}
}

func (accumulator *sseAccumulator) addItem(item map[string]any) {
	itemID := stringValue(item["id"])
	if itemID != "" {
		accumulator.itemByID[itemID] = item
	}
	accumulator.items = upsertSSEItem(accumulator.items, item)
}

func (accumulator *sseAccumulator) addArguments(event map[string]any, arguments string) {
	itemID := stringValue(event["item_id"])
	if itemID == "" {
		return
	}
	item, ok := accumulator.itemByID[itemID]
	if !ok {
		return
	}
	item["arguments"] = arguments
	accumulator.items = upsertSSEItem(accumulator.items, item)
}

func upsertSSEItem(items []any, item map[string]any) []any {
	itemID := stringValue(item["id"])
	if itemID == "" {
		return append(items, item)
	}
	for index, existing := range items {
		existingItem, ok := existing.(map[string]any)
		if ok && stringValue(existingItem["id"]) == itemID {
			items[index] = item
			return items
		}
	}

	return append(items, item)
}

func parseSSEResult(reader io.Reader, onEvent func(StreamEvent)) (*providerResult, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	accumulator, err := scanSSEResponse(scanner, onEvent)
	if err != nil {
		return nil, err
	}
	fallbackText := strings.TrimSpace(strings.Join(accumulator.parts, ""))
	if accumulator.finalResponse != nil {
		result := providerResultFromResponse(accumulator.finalResponse)
		if len(result.OutputItems) == 0 && len(accumulator.items) > 0 {
			result = providerResultFromOutputItems(accumulator.items, fallbackText)
		}
		if strings.TrimSpace(result.Text) == "" {
			result.Text = fallbackText
		}

		return result, nil
	}
	if len(accumulator.items) > 0 {
		return providerResultFromOutputItems(accumulator.items, fallbackText), nil
	}

	return &providerResult{Text: fallbackText, OutputItems: nil, Thinking: nil, ToolCalls: nil}, nil
}

func scanSSEResponse(scanner *bufio.Scanner, onEvent func(StreamEvent)) (accumulator *sseAccumulator, err error) {
	accumulator = newSSEAccumulator()
	for scanner.Scan() {
		event, ok := eventFromSSELine(scanner.Text())
		if ok {
			accumulator.add(event, onEvent)
		}
	}
	if err := scanner.Err(); err != nil {
		return nil, oops.In("assistant").Code("sse_read").Wrapf(err, "read provider stream")
	}

	return accumulator, nil
}

func eventFromSSELine(line string) (map[string]any, bool) {
	trimmed := strings.TrimSpace(line)
	if !strings.HasPrefix(trimmed, "data:") {
		return nil, false
	}
	data := strings.TrimSpace(strings.TrimPrefix(trimmed, "data:"))
	if data == "" || data == "[DONE]" {
		return nil, false
	}

	return decodeEvent([]byte(data))
}

func decodeEvent(data []byte) (map[string]any, bool) {
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return nil, false
	}

	return event, true
}

func providerResultFromResponse(response map[string]any) *providerResult {
	outputItems := outputItemsFromResponse(response[jsonOutputKey])
	text := strings.TrimSpace(extractText(response[jsonOutputKey]))
	if text == "" {
		if outputText, ok := response["output_text"].(string); ok {
			text = strings.TrimSpace(outputText)
		}
	}

	return &providerResult{
		Text:        text,
		OutputItems: outputItems,
		Thinking:    thinkingFromOutput(outputItems),
		ToolCalls:   toolCallsFromOutput(outputItems),
	}
}

func providerResultFromOutputItems(outputItems []any, fallbackText string) *providerResult {
	text := strings.TrimSpace(extractText(outputItems))
	if text == "" {
		text = fallbackText
	}

	return &providerResult{
		Text:        text,
		OutputItems: outputItems,
		Thinking:    thinkingFromOutput(outputItems),
		ToolCalls:   toolCallsFromOutput(outputItems),
	}
}

func outputItemsFromResponse(output any) []any {
	items, ok := output.([]any)
	if !ok {
		return nil
	}
	cloned := make([]any, 0, len(items))
	cloned = append(cloned, items...)

	return cloned
}

func textFromSSEEvent(event map[string]any) (text string, delta bool) {
	eventType := ""
	if value, ok := event[jsonTypeKey].(string); ok {
		eventType = value
	}
	if !isTextDeltaEvent(eventType) {
		return "", false
	}
	if deltaText, ok := event["delta"].(string); ok {
		return deltaText, true
	}
	if eventText, ok := event["text"].(string); ok {
		return eventText, true
	}

	return "", false
}

func isTextDeltaEvent(eventType string) bool {
	return strings.Contains(eventType, "output_text.delta") ||
		strings.Contains(eventType, "text.delta") ||
		strings.Contains(eventType, "content_part.delta")
}

func toolCallsFromOutput(output []any) []toolCall {
	calls := []toolCall{}
	for _, item := range output {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object[jsonTypeKey]) != "function_call" {
			continue
		}
		argumentsJSON := stringValue(object["arguments"])
		arguments := map[string]any{}
		if strings.TrimSpace(argumentsJSON) != "" {
			if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
				arguments = map[string]any{}
			}
		}
		calls = append(calls, toolCall{
			Arguments:     arguments,
			ID:            stringValue(object[jsonCallIDKey]),
			Name:          stringValue(object[jsonToolNameKey]),
			ArgumentsJSON: argumentsJSON,
		})
	}

	return calls
}

func thinkingFromOutput(output []any) []string {
	thinking := []string{}
	for _, item := range output {
		object, ok := item.(map[string]any)
		if !ok || stringValue(object[jsonTypeKey]) != "reasoning" {
			continue
		}
		text := strings.TrimSpace(extractThinkingText(object["summary"]))
		if text != "" {
			thinking = append(thinking, text)
		}
	}

	return thinking
}

func extractThinkingText(value any) string {
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := extractThinkingText(item); text != "" {
				parts = append(parts, text)
			}
		}

		return strings.Join(parts, "\n\n")
	case map[string]any:
		return stringValue(typed["text"])
	case string:
		return typed
	default:
		return ""
	}
}

func stringValue(value any) string {
	switch typed := value.(type) {
	case string:
		return typed
	case fmt.Stringer:
		return typed.String()
	default:
		return ""
	}
}

func extractText(value any) string {
	switch typed := value.(type) {
	case []any:
		parts := make([]string, 0, len(typed))
		for _, item := range typed {
			if text := extractText(item); text != "" {
				parts = append(parts, text)
			}
		}

		return strings.Join(parts, "")
	case map[string]any:
		if text, ok := typed["text"].(string); ok {
			return text
		}
		if content, ok := typed["content"]; ok {
			return extractText(content)
		}
		if output, ok := typed[jsonOutputKey]; ok {
			return extractText(output)
		}
	}

	return ""
}

func codexReasoning(request *CompletionRequest) map[string]string {
	if request.ThinkingLevel == "" || request.ThinkingLevel == thinkingOff {
		return map[string]string{reasoningEffortKey: "none", jsonSummaryKey: reasoningSummaryAuto}
	}

	return map[string]string{reasoningEffortKey: request.ThinkingLevel, jsonSummaryKey: reasoningSummaryAuto}
}

func joinEndpoint(baseURL, suffix string) string {
	baseURL = strings.TrimSpace(baseURL)
	if baseURL == "" {
		baseURL = "https://api.openai.com/v1"
	}
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return strings.TrimRight(baseURL, "/") + suffix
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + suffix

	return parsed.String()
}

func cloneHeaders(headers map[string]string) map[string]string {
	cloned := make(map[string]string, len(headers)+2)
	for key, value := range headers {
		cloned[key] = value
	}

	return cloned
}

type providerError struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code"`
}

func providerStatusError(code string, status int, content []byte) error {
	message := errorMessageFromBytes(content)
	if message == "" {
		message = fmt.Sprintf("provider returned HTTP %d", status)
	}

	return oops.In("assistant").Code(code).With("status", status).Errorf("%s", message)
}

func providerErrorToOops(code string, providerError *providerError) error {
	message := providerError.Message
	if message == "" {
		message = "provider returned an error"
	}

	return oops.In("assistant").
		Code(code).
		With(jsonTypeKey, providerError.Type).
		With("provider_code", providerError.Code).
		Errorf("%s", message)
}

func errorMessageFromBytes(content []byte) string {
	var payload any
	if err := json.Unmarshal(content, &payload); err != nil {
		return strings.TrimSpace(string(content))
	}

	return errorMessage(payload)
}

func errorMessage(value any) string {
	switch typed := value.(type) {
	case map[string]any:
		if message, ok := typed["message"].(string); ok {
			return message
		}
		if nested, ok := typed["error"]; ok {
			return errorMessage(nested)
		}
	case string:
		return typed
	}

	return ""
}

func accountIDFromToken(token string) string {
	parts := strings.Split(token, ".")
	if len(parts) != 3 {
		return ""
	}
	decoded, err := base64URLDecode(parts[1])
	if err != nil {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(decoded, &payload); err != nil {
		return ""
	}
	authClaims, ok := payload["https://api.openai.com/auth"].(map[string]any)
	if !ok {
		return ""
	}
	accountID, ok := authClaims["chatgpt_account_id"].(string)
	if !ok {
		return ""
	}

	return accountID
}

func base64URLDecode(value string) ([]byte, error) {
	return base64RawURLDecode(value)
}

var base64RawURLDecode = func(value string) ([]byte, error) {
	return base64.RawURLEncoding.DecodeString(value)
}

func minPositive(value, fallback int) int {
	if value > 0 && value < fallback {
		return value
	}

	return fallback
}
