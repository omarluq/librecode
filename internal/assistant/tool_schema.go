package assistant

import (
	"encoding/json"

	"github.com/omarluq/librecode/internal/tool"
)

func responseTools(request *CompletionRequest) []map[string]any {
	definitions := requestToolDefinitions(request)
	tools := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, map[string]any{
			jsonTypeKey:        functionToolType,
			jsonToolNameKey:    string(definition.Name),
			jsonDescriptionKey: definition.Description,
			jsonToolParamsKey:  toolParameterSchema(&definition),
			"strict":           false,
		})
	}

	return tools
}

func openAIChatTools(request *CompletionRequest) []map[string]any {
	definitions := requestToolDefinitions(request)
	tools := make([]map[string]any, 0, len(definitions))
	for _, definition := range definitions {
		tools = append(tools, map[string]any{
			jsonTypeKey: functionToolType,
			"function": map[string]any{
				jsonToolNameKey:    string(definition.Name),
				jsonDescriptionKey: definition.Description,
				jsonToolParamsKey:  toolParameterSchema(&definition),
			},
		})
	}

	return tools
}

func requestToolDefinitions(request *CompletionRequest) []tool.Definition {
	if request != nil && request.ToolRegistry != nil {
		return request.ToolRegistry.Definitions()
	}

	return tool.AllDefinitions()
}

func toolArgumentsFromJSON(argumentsJSON string) map[string]any {
	arguments := map[string]any{}
	if argumentsJSON == "" {
		return arguments
	}
	if err := json.Unmarshal([]byte(argumentsJSON), &arguments); err != nil {
		return map[string]any{}
	}

	return arguments
}

var builtinToolSchemas = map[tool.Name]func() map[string]any{
	tool.NameRead:  readToolSchema,
	tool.NameBash:  bashToolSchema,
	tool.NameEdit:  editToolSchema,
	tool.NameWrite: writeToolSchema,
	tool.NameGrep:  grepToolSchema,
	tool.NameFind:  findToolSchema,
	tool.NameLS:    lsToolSchema,
}

func toolParameterSchema(definition *tool.Definition) map[string]any {
	if definition != nil && len(definition.Schema) > 0 {
		return cloneToolSchema(definition.Schema)
	}
	if definition == nil {
		return freeformToolSchema()
	}

	factory, ok := builtinToolSchemas[definition.Name]
	if !ok {
		return freeformToolSchema()
	}
	schema := factory()
	schema["additionalProperties"] = false

	return schema
}

func freeformToolSchema() map[string]any {
	return map[string]any{jsonTypeKey: jsonObjectType, "additionalProperties": true}
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
			jsonCommandKey: stringSchema("Bash command to execute in the current workspace."),
			"timeout":      numberSchema("Optional timeout in seconds."),
		},
		jsonRequiredKey: []string{jsonCommandKey},
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
				jsonOldTextKey: stringSchema(
					"Exact text to replace. Must match a unique, non-overlapping region.",
				),
				jsonNewTextKey: stringSchema("Replacement text."),
			},
			jsonRequiredKey: []string{jsonOldTextKey, jsonNewTextKey},
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
			jsonContentKey: stringSchema("Complete file content to write."),
		},
		jsonRequiredKey: []string{jsonPathKey, jsonContentKey},
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

func cloneToolSchema(schema map[string]any) map[string]any {
	clone := make(map[string]any, len(schema))
	for key, value := range schema {
		clone[key] = value
	}

	return clone
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
