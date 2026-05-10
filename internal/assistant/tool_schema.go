package assistant

import (
	"github.com/omarluq/librecode/internal/tool"
)

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
