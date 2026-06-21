package provider

import (
	"github.com/samber/oops"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/tool"
)

func requestToolDefinitions(request *CompletionRequest) []llm.ToolDefinition {
	if request != nil && request.Request.DisableTools {
		return nil
	}

	if request != nil && len(request.Request.Tools) > 0 {
		return request.Request.Tools
	}

	return builtinToolDefinitions()
}

func builtinToolDefinitions() []llm.ToolDefinition {
	definitions := tool.AllDefinitions()

	tools := make([]llm.ToolDefinition, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, llm.ToolDefinition{
			Schema:      definition.Schema,
			Name:        string(definition.Name),
			Description: definition.Description,
			ReadOnly:    definition.ReadOnly,
		})
	}

	return tools
}

// ResponseToolsFromDefinitions returns OpenAI Responses API tool declarations for definitions.
func ResponseToolsFromDefinitions(definitions []llm.ToolDefinition) []map[string]any {
	return responseTools(definitions)
}

func responseTools(definitions []llm.ToolDefinition) []map[string]any {
	tools := make([]map[string]any, 0, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		tools = append(tools, map[string]any{
			jsonTypeKey:        functionToolType,
			jsonToolNameKey:    definition.Name,
			jsonDescriptionKey: definition.Description,
			jsonToolParamsKey:  toolParameterSchemaForDefinition(definition),
			"strict":           false,
		})
	}

	return tools
}

// OpenAIChatToolsFromDefinitions returns OpenAI Chat Completions tool declarations for definitions.
func OpenAIChatToolsFromDefinitions(definitions []llm.ToolDefinition) []map[string]any {
	return openAIChatTools(definitions)
}

func openAIChatTools(definitions []llm.ToolDefinition) []map[string]any {
	tools := make([]map[string]any, 0, len(definitions))
	for index := range definitions {
		definition := &definitions[index]
		tools = append(tools, map[string]any{
			jsonTypeKey: functionToolType,
			jsonFunctionKey: map[string]any{
				jsonToolNameKey:    definition.Name,
				jsonDescriptionKey: definition.Description,
				jsonToolParamsKey:  toolParameterSchemaForDefinition(definition),
			},
		})
	}

	return tools
}

func toolArgumentsFromJSON(argumentsJSON string) tool.Arguments {
	arguments, err := tool.ArgumentsFromRaw([]byte(argumentsJSON))
	if err != nil {
		return tool.EmptyArguments()
	}

	return arguments
}

type toolParameterSchema struct {
	raw tool.Schema
}

func (schema toolParameterSchema) MarshalJSON() ([]byte, error) {
	encoded := schema.raw.RawMessage()
	if len(encoded) == 0 {
		return []byte("null"), nil
	}

	return encoded, nil
}

func rawToolParameterSchema(schema tool.Schema) toolParameterSchema {
	return toolParameterSchema{raw: schema}
}

func toolParameterSchemaForDefinition(definition *llm.ToolDefinition) toolParameterSchema {
	if definition == nil {
		return rawToolParameterSchema(freeformToolSchema())
	}

	if !definition.Schema.IsEmpty() {
		return rawToolParameterSchema(definition.Schema)
	}

	return rawToolParameterSchema(freeformToolSchema())
}

func freeformToolSchema() tool.Schema {
	schema, err := tool.SchemaFromRaw([]byte(`{"type":"object","additionalProperties":true}`))
	if err != nil {
		panic(oops.In("provider").Code("tool_schema_freeform").Wrapf(err, "build freeform tool schema"))
	}

	return schema
}
