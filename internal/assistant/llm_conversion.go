package assistant

import (
	"strings"

	"github.com/samber/lo"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/llmconv"
	"github.com/omarluq/librecode/internal/mapsutil"
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
		Usage:           llmconv.UsageFromModel(request.Usage),
		DisableTools:    request.DisableTools,
	}
}

func emptyLLMRequest() llm.Request {
	return llm.EmptyRequest()
}

func llmMessagesFromDatabase(messages []database.MessageEntity) []llm.Message {
	if len(messages) == 0 {
		return nil
	}

	return lo.FilterMap(messages, func(message database.MessageEntity, _ int) (llm.Message, bool) {
		return llmMessageFromDatabase(&message)
	})
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

	if registry == nil {
		return llmToolDefinitionsFromTool(tool.AllDefinitions())
	}

	return llmToolDefinitionsFromTool(registry.Definitions())
}

func llmToolDefinitionsFromTool(definitions []tool.Definition) []llm.ToolDefinition {
	return lo.Map(definitions, func(definition tool.Definition, _ int) llm.ToolDefinition {
		return llmToolDefinitionFromTool(&definition)
	})
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
		FinishReason: completionResultFinishReason(result),
		Content:      content,
		ToolCalls:    nil,
		Usage:        llmconv.UsageFromModel(result.Usage),
	}
}

func completionResultFinishReason(result *CompletionResult) llm.FinishReason {
	if result == nil || result.FinishReason == llm.FinishReasonUnknown {
		return llm.FinishReasonStop
	}

	return result.FinishReason
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
		Metadata:      toolIdentityMetadata(event.ParentCallID, event.Sequence),
		ToolCallID:    event.CallID,
		ArgumentsJSON: event.ArgumentsJSON,
		Name:          event.Name,
		Error:         event.Error,
		Content:       []llm.Part{llm.TextPart(event.Result)},
		IsError:       event.IsError,
	}
}

func llmModelRefFromModel(input *model.Model) llm.ModelRef {
	if input == nil {
		return emptyLLMRequest().Model
	}

	return llm.ModelRef{
		Metadata:         cloneAnyMapNil(input.Compat),
		ThinkingLevelMap: thinkingLevelMapToLLM(input.ThinkingLevelMap),
		Provider:         input.Provider,
		ID:               input.ID,
		API:              input.API,
		BaseURL:          input.BaseURL,
		MaxTokens:        input.MaxTokens,
		ContextWindow:    input.ContextWindow,
		Reasoning:        input.Reasoning,
	}
}

func llmAuthFromModel(auth model.RequestAuth) llm.Auth {
	return llm.Auth{Headers: cloneStringMapNil(auth.Headers), APIKey: auth.APIKey}
}

func llmToolDefinitionFromTool(definition *tool.Definition) llm.ToolDefinition {
	if definition == nil {
		return llm.ToolDefinition{Schema: tool.EmptySchema(), Name: "", Description: "", ReadOnly: false}
	}

	return llm.ToolDefinition{
		Schema:      definition.Schema,
		Name:        string(definition.Name),
		Description: definition.Description,
		ReadOnly:    definition.ReadOnly,
	}
}

func thinkingLevelMapToLLM(values map[model.ThinkingLevel]*string) map[string]*string {
	if values == nil {
		return nil
	}

	converted := make(map[string]*string, len(values))
	for level, value := range values {
		converted[string(level)] = value
	}

	return converted
}

func cloneStringMapNil(values map[string]string) map[string]string {
	return mapsutil.ClonePreserveNil(values)
}

func cloneAnyMapNil(values map[string]any) map[string]any {
	return mapsutil.ClonePreserveNil(values)
}
