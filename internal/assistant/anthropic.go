package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
)

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

func anthropicHeaders(request *CompletionRequest) map[string]string {
	headers := cloneHeaders(request.Auth.Headers)
	headers["x-api-key"] = request.Auth.APIKey
	headers["anthropic-version"] = "2023-06-01"

	return headers
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
