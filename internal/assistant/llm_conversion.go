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
		return emptyLLMRequest()
	}

	return llm.Request{
		ProviderOptions: nil,
		Auth:            llmAuthFromModel(request.Auth),
		SystemPrompt:    request.SystemPrompt,
		ThinkingLevel:   request.ThinkingLevel,
		SessionID:       request.SessionID,
		Messages:        llmMessagesFromDatabase(request.Messages),
		Tools:           llmToolDefinitionsFromRegistry(request.ToolRegistry, request.DisableTools),
		Model:           llmModelRefFromModel(&request.Model),
		DisableTools:    request.DisableTools,
	}
}

func emptyLLMRequest() llm.Request {
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
		converted = append(converted, llmToolDefinitionFromTool(&definitions[index]))
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
		Usage:        llmUsageFromModel(result.Usage),
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

func llmUsageFromModel(usage model.TokenUsage) llm.Usage {
	return llm.Usage{
		Breakdown:       cloneIntMap(usage.Breakdown),
		TopContributors: llmTokenContributorsFromModel(usage.TopContributors),
		ContextWindow:   usage.ContextWindow,
		ContextTokens:   usage.ContextTokens,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
	}
}

func llmUsageToModel(usage llm.Usage) model.TokenUsage {
	return model.TokenUsage{
		Breakdown:       cloneIntMap(usage.Breakdown),
		TopContributors: llmTokenContributorsToModel(usage.TopContributors),
		ContextWindow:   usage.ContextWindow,
		ContextTokens:   usage.ContextTokens,
		InputTokens:     usage.InputTokens,
		OutputTokens:    usage.OutputTokens,
	}
}

func llmModelRefFromModel(input *model.Model) llm.ModelRef {
	if input == nil {
		return emptyLLMRequest().Model
	}

	return llm.ModelRef{
		Metadata:      cloneAnyMapNil(input.Compat),
		Provider:      input.Provider,
		ID:            input.ID,
		API:           input.API,
		BaseURL:       input.BaseURL,
		MaxTokens:     input.MaxTokens,
		ContextWindow: input.ContextWindow,
		Reasoning:     input.Reasoning,
	}
}

func llmAuthFromModel(auth model.RequestAuth) llm.Auth {
	return llm.Auth{Headers: cloneStringMapNil(auth.Headers), APIKey: auth.APIKey}
}

func llmToolDefinitionFromTool(definition *tool.Definition) llm.ToolDefinition {
	if definition == nil {
		return llm.ToolDefinition{Schema: nil, Name: "", Description: "", ReadOnly: false}
	}

	return llm.ToolDefinition{
		Schema:      cloneAnyMapNil(definition.Schema),
		Name:        string(definition.Name),
		Description: definition.Description,
		ReadOnly:    definition.ReadOnly,
	}
}

func llmTokenContributorsFromModel(contributors []model.TokenContributor) []llm.TokenContributor {
	if len(contributors) == 0 {
		return nil
	}
	output := make([]llm.TokenContributor, 0, len(contributors))
	for _, contributor := range contributors {
		output = append(output, llm.TokenContributor{
			Label:   contributor.Label,
			Role:    contributor.Role,
			Preview: contributor.Preview,
			Tokens:  contributor.Tokens,
			Chars:   contributor.Chars,
		})
	}

	return output
}

func llmTokenContributorsToModel(contributors []llm.TokenContributor) []model.TokenContributor {
	if len(contributors) == 0 {
		return nil
	}
	output := make([]model.TokenContributor, 0, len(contributors))
	for _, contributor := range contributors {
		output = append(output, model.TokenContributor{
			Label:   contributor.Label,
			Role:    contributor.Role,
			Preview: contributor.Preview,
			Tokens:  contributor.Tokens,
			Chars:   contributor.Chars,
		})
	}

	return output
}

func cloneStringMapNil(values map[string]string) map[string]string {
	if values == nil {
		return nil
	}

	return cloneStringMap(values)
}

func cloneAnyMapNil(values map[string]any) map[string]any {
	if values == nil {
		return nil
	}

	return cloneAnyMap(values)
}
