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
	reasoningEffortKey      = "effort"
	thinkingOff             = "off"
	reasoningSummaryAuto    = "auto"
)

// CompletionRequest describes one model completion request.
type CompletionRequest struct {
	SessionID     string                   `json:"session_id"`
	SystemPrompt  string                   `json:"system_prompt"`
	ThinkingLevel string                   `json:"thinking_level"`
	Auth          model.RequestAuth        `json:"auth"`
	Messages      []database.MessageEntity `json:"messages"`
	Model         model.Model              `json:"model"`
}

// CompletionClient talks to provider APIs.
type CompletionClient interface {
	Complete(ctx context.Context, request *CompletionRequest) (string, error)
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
func (client *HTTPCompletionClient) Complete(ctx context.Context, request *CompletionRequest) (string, error) {
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
		return "", oops.In("assistant").
			Code("unsupported_provider_api").
			With("api", api).
			Errorf("provider api is not implemented")
	}
}

func (client *HTTPCompletionClient) completeOpenAIChat(
	ctx context.Context,
	request *CompletionRequest,
) (string, error) {
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
		return "", err
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
		return "", oops.In("assistant").Code("openai_chat_decode").Wrapf(err, "decode chat response")
	}
	if response.Error.Message != "" {
		return "", providerErrorToOops("openai_chat_error", &response.Error)
	}
	if len(response.Choices) == 0 || strings.TrimSpace(response.Choices[0].Message.Content) == "" {
		return "", oops.In("assistant").Code("openai_chat_empty").Errorf("provider returned an empty response")
	}

	return strings.TrimSpace(response.Choices[0].Message.Content), nil
}

func (client *HTTPCompletionClient) completeOpenAIResponses(
	ctx context.Context,
	request *CompletionRequest,
) (string, error) {
	payload := map[string]any{
		jsonModelKey: request.Model.ID,
		"input":      openAIResponseInput(request.Messages),
		"store":      false,
	}
	if request.SystemPrompt != "" {
		payload["instructions"] = request.SystemPrompt
	}
	if request.Model.Reasoning && request.ThinkingLevel != "" && request.ThinkingLevel != thinkingOff {
		payload["reasoning"] = map[string]any{
			reasoningEffortKey: request.ThinkingLevel,
			jsonSummaryKey:     reasoningSummaryAuto,
		}
	}
	endpoint := joinEndpoint(request.Model.BaseURL, "/responses")
	content, err := client.postJSON(ctx, endpoint, openAIHeaders(request), payload)
	if err != nil {
		return "", err
	}
	text, err := parseOpenAIResponse(content)
	if err != nil {
		return "", err
	}

	return text, nil
}

func (client *HTTPCompletionClient) completeOpenAICodex(
	ctx context.Context,
	request *CompletionRequest,
) (string, error) {
	payload := map[string]any{
		jsonModelKey:          request.Model.ID,
		"store":               false,
		"stream":              true,
		"instructions":        request.SystemPrompt,
		"input":               openAIResponseInput(request.Messages),
		"text":                map[string]string{"verbosity": "low"},
		"include":             []string{"reasoning.encrypted_content"},
		"prompt_cache_key":    request.SessionID,
		"tool_choice":         reasoningSummaryAuto,
		"parallel_tool_calls": true,
		"reasoning":           codexReasoning(request),
	}
	endpoint := joinEndpoint(request.Model.BaseURL, "/codex/responses")
	httpRequest, err := jsonRequest(ctx, endpoint, codexHeaders(request), payload)
	if err != nil {
		return "", err
	}
	response, err := client.client.Do(httpRequest)
	if err != nil {
		return "", oops.In("assistant").Code("codex_http").Wrapf(err, "request codex response")
	}
	defer closeBody(response.Body)
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		content, readErr := io.ReadAll(response.Body)
		if readErr != nil {
			return "", oops.In("assistant").Code("codex_error_read").Wrapf(readErr, "read codex error")
		}

		return "", providerStatusError("codex_status", response.StatusCode, content)
	}

	return parseSSEText(response.Body)
}

func (client *HTTPCompletionClient) completeAnthropic(ctx context.Context, request *CompletionRequest) (string, error) {
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
		return "", err
	}
	var response struct {
		Error   providerError `json:"error"`
		Content []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		} `json:"content"`
	}
	if err := json.Unmarshal(content, &response); err != nil {
		return "", oops.In("assistant").Code("anthropic_decode").Wrapf(err, "decode anthropic response")
	}
	if response.Error.Message != "" {
		return "", providerErrorToOops("anthropic_error", &response.Error)
	}
	parts := make([]string, 0, len(response.Content))
	for _, block := range response.Content {
		if block.Type == "text" && block.Text != "" {
			parts = append(parts, block.Text)
		}
	}
	text := strings.TrimSpace(strings.Join(parts, "\n"))
	if text == "" {
		return "", oops.In("assistant").Code("anthropic_empty").Errorf("provider returned an empty response")
	}

	return text, nil
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

func openAIResponseInput(messages []database.MessageEntity) []map[string]any {
	input := []map[string]any{}
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

func openAIRole(role database.Role) (string, bool) {
	switch role {
	case database.RoleUser:
		return "user", true
	case database.RoleAssistant:
		return "assistant", true
	case database.RoleToolResult,
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
		return "user", true
	case database.RoleAssistant:
		return "assistant", true
	case database.RoleToolResult,
		database.RoleCustom,
		database.RoleBashExecution,
		database.RoleBranchSummary,
		database.RoleCompactionSummary:
		return "", false
	}

	return "", false
}

func parseOpenAIResponse(content []byte) (string, error) {
	var response map[string]any
	if err := json.Unmarshal(content, &response); err != nil {
		return "", oops.In("assistant").Code("openai_response_decode").Wrapf(err, "decode response")
	}
	if errorValue, ok := response["error"]; ok {
		message := errorMessage(errorValue)
		if message != "" {
			return "", oops.In("assistant").Code("openai_response_error").Errorf("%s", message)
		}
	}
	if text, ok := response["output_text"].(string); ok && strings.TrimSpace(text) != "" {
		return strings.TrimSpace(text), nil
	}
	text := strings.TrimSpace(extractText(response["output"]))
	if text == "" {
		return "", oops.In("assistant").Code("openai_response_empty").Errorf("provider returned an empty response")
	}

	return text, nil
}

func parseSSEText(reader io.Reader) (string, error) {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 8*1024*1024)
	parts := []string{}
	finalText := ""
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if !strings.HasPrefix(line, "data:") {
			continue
		}
		data := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if data == "" || data == "[DONE]" {
			continue
		}
		text, delta := textFromSSEData([]byte(data))
		if text == "" {
			continue
		}
		if delta {
			parts = append(parts, text)
		} else {
			finalText = text
		}
	}
	if err := scanner.Err(); err != nil {
		return "", oops.In("assistant").Code("sse_read").Wrapf(err, "read provider stream")
	}
	text := strings.TrimSpace(strings.Join(parts, ""))
	if text == "" {
		text = strings.TrimSpace(finalText)
	}
	if text == "" {
		return "", oops.In("assistant").Code("sse_empty").Errorf("provider returned an empty response")
	}

	return text, nil
}

func textFromSSEData(data []byte) (text string, delta bool) {
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		return "", false
	}
	eventType := ""
	if value, ok := event["type"].(string); ok {
		eventType = value
	}
	if deltaText, ok := event["delta"].(string); ok {
		return deltaText, true
	}
	if strings.Contains(eventType, "delta") {
		if eventText, ok := event["text"].(string); ok {
			return eventText, true
		}
	}
	if response, ok := event["response"].(map[string]any); ok {
		return extractText(response["output"]), false
	}

	return extractText(event["output"]), false
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
		if output, ok := typed["output"]; ok {
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
		With("type", providerError.Type).
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
