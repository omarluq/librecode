package assistant

import (
	"context"
	"encoding/json"
	"strings"

	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/database"
)

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
		Error   providerError  `json:"error"`
		Usage   map[string]any `json:"usage"`
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

	return textCompletionResult(response.Choices[0].Message.Content, usageFromObject(response.Usage)), nil
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
