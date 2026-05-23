package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

type toolSchemaCase struct {
	assertion  func(*testing.T, map[string]any)
	definition *tool.Definition
	name       string
}

func TestToolParameterSchema(t *testing.T) {
	t.Parallel()

	customSchema := map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			"message": map[string]any{jsonTypeKey: "string"},
		},
	}
	tests := []toolSchemaCase{
		freeformSchemaCase("nil definition is freeform", nil),
		customToolSchemaCase(customSchema),
		strictReadToolSchemaCase(),
		freeformSchemaCase("unknown definition is freeform", echoToolDefinition(nil)),
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			schema := toolParameterSchema(testCase.definition)

			require.NotNil(t, schema)
			testCase.assertion(t, schema)
		})
	}
}

func freeformSchemaCase(name string, definition *tool.Definition) toolSchemaCase {
	return toolSchemaCase{
		assertion: func(t *testing.T, schema map[string]any) {
			t.Helper()
			assert.Equal(t, true, schema["additionalProperties"])
		},
		definition: definition,
		name:       name,
	}
}

func customToolSchemaCase(customSchema map[string]any) toolSchemaCase {
	return toolSchemaCase{
		assertion: func(t *testing.T, schema map[string]any) {
			t.Helper()
			assert.Equal(t, customSchema, schema)
			schema["mutated"] = true
			assert.NotContains(t, customSchema, "mutated")
		},
		definition: echoToolDefinition(customSchema),
		name:       "custom schema is cloned",
	}
}

func strictReadToolSchemaCase() toolSchemaCase {
	return toolSchemaCase{
		assertion: func(t *testing.T, schema map[string]any) {
			t.Helper()
			assert.Equal(t, false, schema["additionalProperties"])
			properties, ok := schema[jsonPropertiesKey].(map[string]any)
			require.True(t, ok)
			assert.Contains(t, properties, jsonPathKey)
		},
		definition: &tool.Definition{
			Schema:           nil,
			Name:             tool.NameRead,
			Label:            "read",
			Description:      "Read file",
			PromptSnippet:    "",
			PromptGuidelines: []string{},
			ReadOnly:         true,
		},
		name: "built-in schema is strict",
	}
}

func echoToolDefinition(schema map[string]any) *tool.Definition {
	return &tool.Definition{
		Schema:           schema,
		Name:             tool.Name("echo"),
		Label:            "echo",
		Description:      "Echo text",
		PromptSnippet:    "",
		PromptGuidelines: []string{},
		ReadOnly:         false,
	}
}
