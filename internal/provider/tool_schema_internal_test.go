package provider

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
)

func TestToolParameterSchemaFallbacksAndCloning(t *testing.T) {
	t.Parallel()

	t.Run("nil definition is freeform", func(t *testing.T) {
		t.Parallel()

		schema := ToolParameterSchema(nil)

		assert.JSONEq(t, jsonString(jsonObjectType), jsonString(schema[jsonTypeKey]))
		assertAdditionalProperties(t, schema, true)
	})

	t.Run("unknown definition is freeform", func(t *testing.T) {
		t.Parallel()

		schema := ToolParameterSchema(newToolDefinitionForSchemaTest("custom", nil))

		assert.JSONEq(t, jsonString(jsonObjectType), jsonString(schema[jsonTypeKey]))
		assertAdditionalProperties(t, schema, true)
	})

	t.Run("custom schema is cloned", func(t *testing.T) {
		t.Parallel()

		original := map[string]any{jsonTypeKey: jsonObjectType}
		schema := ToolParameterSchema(newToolDefinitionForSchemaTest("custom", original))

		schema[jsonTypeKey] = "changed"
		assert.JSONEq(t, jsonString(jsonObjectType), jsonString(original[jsonTypeKey]))
	})

	t.Run("builtin schema is strict", func(t *testing.T) {
		t.Parallel()

		schema := ToolParameterSchema(newToolDefinitionForSchemaTest(jsonReadToolName, nil))

		assert.JSONEq(t, jsonString(jsonObjectType), jsonString(schema[jsonTypeKey]))
		assertAdditionalProperties(t, schema, false)
	})
}

func TestRequestToolDefinitions(t *testing.T) {
	t.Parallel()

	t.Run("disabled", func(t *testing.T) {
		t.Parallel()

		definitions := requestToolDefinitions(&CompletionRequest{
			OnProviderObserve: nil,
			OnProviderRequest: nil,
			ExecuteTools:      nil,
			OnEvent:           nil,
			Request: llm.Request{
				ProviderOptions: nil,
				Auth:            llm.Auth{Headers: nil, APIKey: ""},
				SystemPrompt:    "",
				ThinkingLevel:   "",
				SessionID:       "",
				Messages:        nil,
				Tools:           nil,
				Model:           emptyModelRef(),
				Usage:           llm.EmptyUsage(),
				DisableTools:    true,
			},
			ProviderAttempt: 0,
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

func assertAdditionalProperties(t *testing.T, schema map[string]any, expected bool) {
	t.Helper()

	actual, ok := schema["additionalProperties"].(bool)
	require.True(t, ok)
	assert.Equal(t, expected, actual)
}

func newToolDefinitionForSchemaTest(name string, schema map[string]any) *llm.ToolDefinition {
	return &llm.ToolDefinition{
		Schema:      schema,
		Name:        name,
		Description: "description",
		ReadOnly:    false,
	}
}

func TestSchemaPrimitiveHelpers(t *testing.T) {
	t.Parallel()

	assert.Equal(t, map[string]any{
		jsonTypeKey:        jsonStringType,
		jsonDescriptionKey: "description",
		"enum":             []string{"a", "b"},
	}, enumStringSchema("description", []string{"a", "b"}))
}
