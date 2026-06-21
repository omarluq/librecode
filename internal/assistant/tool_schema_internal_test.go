package assistant

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/provider"
	"github.com/omarluq/librecode/internal/tool"
)

type toolSchemaCase struct {
	assertion  func(*testing.T, map[string]any)
	definition *llm.ToolDefinition
	name       string
}

func TestToolParameterSchema(t *testing.T) {
	t.Parallel()

	customSchema := map[string]any{
		jsonTypeKey: jsonObjectType,
		jsonPropertiesKey: map[string]any{
			"message": map[string]any{jsonTypeKey: jsonStringType},
		},
	}
	tests := []toolSchemaCase{
		freeformSchemaCase("nil definition is freeform", nil),
		customToolSchemaCase(customSchema),
		strictReadToolSchemaCase(),
		strictASTToolSchemaCase(),
		freeformSchemaCase("unknown definition is freeform", echoToolDefinition(nil)),
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			schema := provider.ToolParameterSchema(testCase.definition)

			require.NotNil(t, schema)
			testCase.assertion(t, schema)
		})
	}
}

func freeformSchemaCase(name string, definition *llm.ToolDefinition) toolSchemaCase {
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
		definition: &llm.ToolDefinition{
			Schema:      tool.EmptySchema(),
			Name:        jsonReadToolName,
			Description: "Read file",
			ReadOnly:    true,
		},
		name: "built-in schema is strict",
	}
}

func strictASTToolSchemaCase() toolSchemaCase {
	return toolSchemaCase{
		assertion: func(t *testing.T, schema map[string]any) {
			t.Helper()

			properties, ok := schema[jsonPropertiesKey].(map[string]any)
			require.True(t, ok)
			mode, ok := properties["mode"].(map[string]any)
			require.True(t, ok)
			assert.JSONEq(
				t,
				`["outline","symbols","query","node","tree"]`,
				mustMarshalToolSchemaTestValue(t, mode["enum"]),
			)
		},
		definition: &llm.ToolDefinition{
			Schema:      tool.EmptySchema(),
			Name:        "ast",
			Description: "Inspect syntax trees",
			ReadOnly:    true,
		},
		name: "ast schema constrains mode enum",
	}
}

func echoToolDefinition(schema map[string]any) *llm.ToolDefinition {
	return &llm.ToolDefinition{
		Schema:      tool.MustSchemaFromMap(schema),
		Name:        "echo",
		Description: "Echo",
		ReadOnly:    false,
	}
}

func mustMarshalToolSchemaTestValue(t *testing.T, value any) string {
	t.Helper()

	encoded, err := json.Marshal(value)
	require.NoError(t, err)

	return string(encoded)
}
