package assistant

import (
	"strings"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

func llmRequestFromCompletionRequest(request *CompletionRequest) llm.Request {
	if request == nil {
		return llm.Request{
			ProviderOptions: nil,
			Auth:            llm.Auth{Headers: nil, APIKey: ""},
			SystemPrompt:    "",
			ThinkingLevel:   "",
			SessionID:       "",
			Messages:        nil,
			Tools:           nil,
			Model: llm.ModelRef{
				Metadata:      nil,
				Provider:      "",
				ID:            "",
				API:           "",
				BaseURL:       "",
				MaxTokens:     0,
				ContextWindow: 0,
				Reasoning:     false,
			},
			DisableTools: false,
		}
	}

	return llm.Request{
		ProviderOptions: nil,
		Auth:            llm.AuthFromModel(request.Auth),
		SystemPrompt:    request.SystemPrompt,
		ThinkingLevel:   request.ThinkingLevel,
		SessionID:       request.SessionID,
		Messages:        llmMessagesFromDatabase(request.Messages),
		Tools:           llmToolDefinitionsFromRegistry(request.ToolRegistry, request.DisableTools),
		Model:           llm.ModelRefFromModel(&request.Model),
		DisableTools:    request.DisableTools,
	}
}

func llmMessagesFromDatabase(messages []database.MessageEntity) []llm.Message {
	if len(messages) == 0 {
		return nil
	}
	converted := make([]llm.Message, 0, len(messages))
	for index := range messages {
		message, ok := llmMessageFromDatabase(&messages[index])
		if !ok {
			continue
		}
		converted = append(converted, message)
	}

	return converted
}

func llmMessageFromDatabase(message *database.MessageEntity) (llm.Message, bool) {
	if message == nil || strings.TrimSpace(message.Content) == "" {
		return emptyLLMMessage(), false
	}
	role, ok := llmRoleFromDatabase(message.Role)
	if !ok {
		return emptyLLMMessage(), false
	}

	return llm.TextMessage(role, message.Content), true
}

func emptyLLMMessage() llm.Message {
	return llm.Message{Metadata: nil, Role: "", Content: nil}
}

func llmRoleFromDatabase(role database.Role) (llm.Role, bool) {
	switch role {
	case database.RoleUser,
		database.RoleBranchSummary,
		database.RoleCompactionSummary,
		database.RoleCustom,
		database.RoleBashExecution:
		return llm.RoleUser, true
	case database.RoleAssistant:
		return llm.RoleAssistant, true
	case database.RoleToolResult:
		return llm.RoleTool, true
	case database.RoleThinking:
		return llm.RoleAssistant, true
	}

	return "", false
}

func llmToolDefinitionsFromRegistry(registry *tool.Registry, disabled bool) []llm.ToolDefinition {
	if disabled {
		return nil
	}
	definitions := tool.AllDefinitions()
	if registry != nil {
		definitions = registry.Definitions()
	}
	converted := make([]llm.ToolDefinition, 0, len(definitions))
	for index := range definitions {
		converted = append(converted, llm.ToolDefinitionFromTool(&definitions[index]))
	}

	return converted
}

func llmResponseFromCompletionResult(result *CompletionResult) llm.Response {
	if result == nil {
		return llm.Response{
			FinishReason: llm.FinishReasonUnknown,
			Content:      nil,
			ToolCalls:    nil,
			Usage:        llm.EmptyUsage(),
		}
	}
	content := []llm.Part{}
	for _, thinking := range result.Thinking {
		trimmed := strings.TrimSpace(thinking)
		if trimmed == "" {
			continue
		}
		content = append(content, llm.Part{
			Metadata:   nil,
			ToolCall:   nil,
			ToolResult: nil,
			Type:       llm.PartReasoning,
			Text:       trimmed,
			Data:       "",
			MIMEType:   "",
		})
	}
	if strings.TrimSpace(result.Text) != "" {
		content = append(content, llm.TextPart(result.Text))
	}
	for index := range result.ToolEvents {
		content = append(content, llmToolResultPart(&result.ToolEvents[index]))
	}

	return llm.Response{
		FinishReason: llm.FinishReasonStop,
		Content:      content,
		ToolCalls:    nil,
		Usage:        llm.UsageFromModel(result.Usage),
	}
}

func llmToolResultPart(event *ToolEvent) llm.Part {
	return llm.Part{
		Metadata:   nil,
		ToolCall:   nil,
		ToolResult: llmToolResultFromEvent(event),
		Type:       llm.PartToolResult,
		Text:       "",
		Data:       "",
		MIMEType:   "",
	}
}

func llmToolResultFromEvent(event *ToolEvent) *llm.ToolResult {
	if event == nil {
		return &llm.ToolResult{
			Metadata:      nil,
			ToolCallID:    "",
			ArgumentsJSON: "",
			Name:          "",
			Error:         "",
			Content:       nil,
			IsError:       false,
		}
	}

	return &llm.ToolResult{
		Metadata:      nil,
		ToolCallID:    "",
		ArgumentsJSON: event.ArgumentsJSON,
		Name:          event.Name,
		Error:         event.Error,
		Content:       []llm.Part{llm.TextPart(event.Result)},
		IsError:       event.IsError,
	}
}

func llmUsageToModel(usage llm.Usage) model.TokenUsage {
	return llm.UsageToModel(usage)
}
