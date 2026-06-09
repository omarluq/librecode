package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/tool"
)

func TestToolParameterSchemaFallbacksAndCloning(t *testing.T) {
	t.Parallel()

	t.Run("nil definition is freeform", func(t *testing.T) {
		t.Parallel()

		schema := ToolParameterSchema(nil)

		assert.Equal(t, jsonObjectType, schema[jsonTypeKey])
		assertAdditionalProperties(t, schema, true)
	})

	t.Run("unknown definition is freeform", func(t *testing.T) {
		t.Parallel()

		schema := ToolParameterSchema(newToolDefinitionForSchemaTest(tool.Name("custom"), nil))

		assert.Equal(t, jsonObjectType, schema[jsonTypeKey])
		assertAdditionalProperties(t, schema, true)
	})

	t.Run("custom schema is cloned", func(t *testing.T) {
		t.Parallel()

		original := map[string]any{jsonTypeKey: jsonObjectType}
		schema := ToolParameterSchema(newToolDefinitionForSchemaTest(tool.Name("custom"), original))

		schema[jsonTypeKey] = "changed"
		assert.Equal(t, jsonObjectType, original[jsonTypeKey])
	})

	t.Run("builtin schema is strict", func(t *testing.T) {
		t.Parallel()

		schema := ToolParameterSchema(newToolDefinitionForSchemaTest(tool.NameRead, nil))

		assert.Equal(t, jsonObjectType, schema[jsonTypeKey])
		assertAdditionalProperties(t, schema, false)
	})
}

func TestRequestToolDefinitions(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()

		definitions := requestToolDefinitions(&CompletionRequest{
			OnEvent:           nil,
			OnProviderObserve: nil,
			OnProviderRequest: nil,
			OnToolCall:        nil,
			OnToolResult:      nil,
			ToolRegistry:      tool.NewRegistry(t.TempDir()),
			ExecuteTools:      nil,
			SessionID:         "",
			SystemPrompt:      "",
			ThinkingLevel:     "",
			CWD:               "",
			Auth:              emptyRequestAuth(),
			Messages:          nil,
			Usage:             model.EmptyTokenUsage(),
			Model:             emptyModel(),
			ProviderAttempt:   0,
			DisableTools:      true,
		})

		assert.Empty(t, definitions)
	})

	t.Run("nil request uses builtins", func(t *testing.T) {
		t.Parallel()

		definitions := requestToolDefinitions(nil)

		assert.NotEmpty(t, definitions)
	})
}

func TestToolArgumentsFromJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		want map[string]any
		name string
		json string
	}{
		{name: "empty", json: "", want: map[string]any{}},
		{name: "invalid", json: "{", want: map[string]any{}},
		{name: "object", json: `{"path":"` + testToolPath + `"}`, want: map[string]any{jsonPathKey: testToolPath}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, test.want, toolArgumentsFromJSON(test.json))
		})
	}
}

func TestToolSchemaFactoriesIncludeExpectedShape(t *testing.T) {
	t.Parallel()

	assertRequired := func(t *testing.T, schema map[string]any, required ...string) {
		t.Helper()

		actual, ok := schema[jsonRequiredKey].([]string)
		require.True(t, ok)
		assert.ElementsMatch(t, required, actual)
	}

	assertRequired(t, bashToolSchema(), jsonCommandKey)
	assertRequired(t, writeToolSchema(), jsonPathKey, jsonContentKey)
	assertRequired(t, grepToolSchema(), jsonPatternKey)
	assertRequired(t, findToolSchema(), jsonPatternKey)
	assertRequired(t, astToolSchema(), jsonPathKey)

	properties, ok := astToolSchema()[jsonPropertiesKey].(map[string]any)
	require.True(t, ok)
	astMode, ok := properties["mode"].(map[string]any)
	require.True(t, ok)
	assert.ElementsMatch(t, []string{"outline", "symbols", "query", "node", "tree"}, astMode["enum"])
}

func TestSchemaPrimitiveHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, map[string]any{
		jsonTypeKey:        jsonStringType,
		jsonDescriptionKey: "description",
		"enum":             []string{"a", "b"},
	}, enumStringSchema("description", []string{"a", "b"}))
	assert.Equal(t, "integer", integerSchema("n")[jsonTypeKey])
	assert.Equal(t, "number", numberSchema("n")[jsonTypeKey])
	assert.Equal(t, "boolean", booleanSchema("b")[jsonTypeKey])
}

func newToolDefinitionForSchemaTest(name tool.Name, schema map[string]any) *tool.Definition {
	return &tool.Definition{
		Schema:           schema,
		Name:             name,
		Label:            string(name),
		Description:      string(name) + " tool",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         false,
	}
}

func assertAdditionalProperties(t *testing.T, schema map[string]any, expected bool) {
	t.Helper()

	actual, ok := schema["additionalProperties"].(bool)
	require.True(t, ok)
	assert.Equal(t, expected, actual)
}
