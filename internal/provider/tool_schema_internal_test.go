package provider

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/llm"
	"github.com/omarluq/librecode/internal/testutil"
	"github.com/omarluq/librecode/internal/tool"
)

func TestToolParameterSchemaFallbacksAndCloning(t *testing.T) {
	t.Parallel()

	t.Run("unknown definition is freeform", func(t *testing.T) {
		t.Parallel()

		schema := schemaPayloadForDefinition(t, newToolDefinitionForSchemaTest("custom", nil))

		assert.JSONEq(t, jsonString(jsonObjectType), jsonString(schema[jsonTypeKey]))
		assertAdditionalProperties(t, schema, true)
	})

	t.Run("nil definition is freeform", func(t *testing.T) {
		t.Parallel()

		schema := schemaPayloadForDefinition(t, nil)

		assert.JSONEq(t, jsonString(jsonObjectType), jsonString(schema[jsonTypeKey]))
		assertAdditionalProperties(t, schema, true)
	})

	t.Run("custom schema is cloned", func(t *testing.T) {
		t.Parallel()

		original := map[string]any{jsonTypeKey: jsonObjectType}
		schema := schemaPayloadForDefinition(t, newToolDefinitionForSchemaTest("custom", original))

		schema[jsonTypeKey] = "changed"
		assert.JSONEq(t, jsonString(jsonObjectType), jsonString(original[jsonTypeKey]))
	})

	t.Run("builtin schema is strict", func(t *testing.T) {
		t.Parallel()

		definition := builtinDefinitionForSchemaTest(t, jsonReadToolName)
		schema := schemaPayloadForDefinition(t, &definition)

		assert.JSONEq(t, jsonString(jsonObjectType), jsonString(schema[jsonTypeKey]))
		assertAdditionalProperties(t, schema, false)
	})
}

func TestToolDeclarationsFromDefinitions(t *testing.T) {
	t.Parallel()

	definition := llm.ToolDefinition{
		Schema:      tool.EmptySchema(),
		Name:        "custom",
		Description: "custom description",
		ReadOnly:    false,
	}

	tests := []struct {
		declare func([]llm.ToolDefinition) []map[string]any
		assert  func(*testing.T, map[string]any)
		name    string
	}{
		{
			name:    "responses",
			declare: ResponseToolsFromDefinitions,
			assert: func(t *testing.T, declaration map[string]any) {
				t.Helper()

				assert.Equal(t, functionToolType, declaration[jsonTypeKey])
				assert.Equal(t, "custom", declaration[jsonToolNameKey])
			},
		},
		{
			name:    "chat",
			declare: OpenAIChatToolsFromDefinitions,
			assert: func(t *testing.T, declaration map[string]any) {
				t.Helper()

				function, ok := declaration[jsonFunctionKey].(map[string]any)
				require.True(t, ok)
				assert.Equal(t, "custom", function[jsonToolNameKey])
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			declarations := testCase.declare([]llm.ToolDefinition{definition})
			require.Len(t, declarations, 1)
			testCase.assert(t, declarations[0])
		})
	}
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

			assert.Equal(t, test.want, testutil.ToolArgumentFields(toolArgumentsFromJSON(test.json)))
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
		Schema:      schemaFromTestMap(schema),
		Name:        name,
		Description: "description",
		ReadOnly:    false,
	}
}

func builtinDefinitionForSchemaTest(t *testing.T, name string) llm.ToolDefinition {
	t.Helper()

	for _, definition := range builtinToolDefinitions() {
		if definition.Name == name {
			return definition
		}
	}

	t.Fatalf("builtin definition %q not found", name)

	return llm.ToolDefinition{Schema: tool.EmptySchema(), Name: "", Description: "", ReadOnly: false}
}

func schemaFromTestMap(value map[string]any) tool.Schema {
	if value == nil {
		return tool.EmptySchema()
	}

	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	schema, err := tool.SchemaFromRaw(encoded)
	if err != nil {
		panic(err)
	}

	return schema
}

func schemaPayloadForDefinition(t *testing.T, definition *llm.ToolDefinition) map[string]any {
	t.Helper()

	encoded, err := json.Marshal(toolParameterSchemaForDefinition(definition))
	require.NoError(t, err)

	var schema map[string]any
	require.NoError(t, json.Unmarshal(encoded, &schema))

	return schema
}

func TestBuiltinToolSchemaUsesGeneratedStructMetadata(t *testing.T) {
	t.Parallel()

	definition := builtinDefinitionForSchemaTest(t, jsonASTToolName)
	schema := schemaPayloadForDefinition(t, &definition)
	properties, ok := schema[jsonPropertiesKey].(map[string]any)
	require.True(t, ok)

	mode, ok := properties["mode"].(map[string]any)
	require.True(t, ok)
	assert.JSONEq(t, `["outline","symbols","query","node","tree"]`, jsonString(mode["enum"]))
	assert.Equal(
		t,
		"Inspection mode: 'outline' (default), 'symbols', 'query', 'node', or 'tree'.",
		mode[jsonDescriptionKey],
	)
}
