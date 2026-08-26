package assistant

import (
	"encoding/base64"
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
		Usage:           llmconv.UsageFromModel(&request.Usage),
		MaxTokens:       request.MaxTokens,
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
	if message == nil {
		return emptyLLMMessage(), false
	}

	role, ok := llmRoleFromDatabase(message.Role)
	if !ok {
		return emptyLLMMessage(), false
	}

	parts := llmPartsFromDatabase(message.Parts)

	if len(parts) == 0 && strings.TrimSpace(message.Content) != "" {
		parts = append(parts, llm.TextPart(message.Content))
	}

	if len(parts) == 0 {
		return emptyLLMMessage(), false
	}

	return llm.Message{Metadata: nil, Role: role, Content: parts}, true
}

func llmPartsFromDatabase(databaseParts []database.MessagePartEntity) []llm.Part {
	parts := make([]llm.Part, 0, len(databaseParts))
	for index := range databaseParts {
		part := &databaseParts[index]
		if part.Type == database.MessagePartText && strings.TrimSpace(part.Text) != "" {
			parts = append(parts, llm.TextPart(part.Text))
		}

		if part.Type == database.MessagePartImage && len(part.Data) != 0 {
			parts = append(parts, llm.Part{
				Metadata: imagePartMetadata(part.Name, part.Width, part.Height),
				ToolCall: nil, ToolResult: nil, Type: llm.PartImage, Text: "",
				Data: base64.StdEncoding.EncodeToString(part.Data), MIMEType: part.MIMEType,
			})
		}
	}

	return parts
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
