package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRequiredToolArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		definition *Definition
		name       string
		want       []string
	}{
		{
			name:       "nil definition",
			definition: nil,
			want:       nil,
		},
		{
			name: "schema required string slice",
			definition: &Definition{
				Schema:           testSchema(`{"required":["alpha","beta"]}`),
				Name:             NameRead,
				Label:            string(NameRead),
				Description:      "",
				PromptSnippet:    "",
				PromptGuidelines: nil,
				ReadOnly:         true,
			},
			want: []string{"alpha", "beta"},
		},
		{
			name: "schema required slice filters invalid names",
			definition: &Definition{
				Schema:           testSchema(`{"required":["path"," ",42,"limit"]}`),
				Name:             NameRead,
				Label:            string(NameRead),
				Description:      "",
				PromptSnippet:    "",
				PromptGuidelines: nil,
				ReadOnly:         true,
			},
			want: []string{toolInputPathKey, "limit"},
		},
		{
			name: "empty schema has no required arguments",
			definition: &Definition{
				Schema:           EmptySchema(),
				Name:             Name("custom"),
				Label:            "custom",
				Description:      "",
				PromptSnippet:    "",
				PromptGuidelines: nil,
				ReadOnly:         false,
			},
			want: nil,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			assert.Equal(t, testCase.want, requiredToolArguments(testCase.definition))
		})
	}
}

func TestSchemaRequiredArguments(t *testing.T) {
	t.Parallel()

	tests := []struct {
		schema Schema
		name   string
		want   []string
		found  bool
	}{
		{name: "empty schema", schema: EmptySchema(), want: nil, found: false},
		{
			name:   "missing required",
			schema: testSchema(`{"type":"object"}`),
			want:   nil,
			found:  false,
		},
		{
			name:   "invalid required",
			schema: testSchema(`{"required":"path"}`),
			want:   nil,
			found:  false,
		},
		{
			name:   "string slice",
			schema: testSchema(`{"required":["path"]}`),
			want:   []string{toolInputPathKey},
			found:  true,
		},
		{
			name:   "mixed required array filters invalid names",
			schema: testSchema(`{"required":["path","",false,"content"]}`),
			want:   []string{toolInputPathKey, toolInputContentKey},
			found:  true,
		},
	}
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			got, found := schemaRequiredArguments(testCase.schema)
			assert.Equal(t, testCase.found, found)
			assert.Equal(t, testCase.want, got)
		})
	}
}

func TestValidateToolInput(t *testing.T) {
	t.Parallel()

	assert.NoError(t, validateToolInput(nil, EmptyArguments(), nil))
	assert.NoError(t, validateToolInput(&Definition{
		Schema:           EmptySchema(),
		Name:             Name("custom"),
		Label:            "custom",
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         false,
	}, EmptyArguments(), nil))

	definition := Definition{
		Schema: testSchema(`{
			"additionalProperties": false,
			"properties": {
				"path": {"type":"string"},
				"content": {"type":"string"}
			},
			"required": ["path", "content"],
			"type": "object"
		}`),
		Name:             NameWrite,
		Label:            string(NameWrite),
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         false,
	}

	tests := []struct {
		inputJSON   string
		name        string
		wantErrText string
	}{
		{
			name:        "valid input",
			inputJSON:   `{"path":"x","content":""}`,
			wantErrText: "",
		},
		{
			name:        "missing required keeps user friendly error",
			inputJSON:   `{"content":"x"}`,
			wantErrText: "write path is required",
		},
		{
			name:        "invalid type uses schema validation",
			inputJSON:   `{"path":42,"content":"x"}`,
			wantErrText: "validate tool input",
		},
		{
			name:        "unknown property uses schema validation",
			inputJSON:   `{"path":"x","content":"","extra":true}`,
			wantErrText: "validate tool input",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			input := testArguments(testCase.inputJSON)

			err := validateToolInput(&definition, input, nil)
			if testCase.wantErrText == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErrText)
		})
	}
}

func TestValidateToolInputUsesSchemaValidatorCache(t *testing.T) {
	t.Parallel()

	definition := Definition{
		Schema: testSchema(`{
			"additionalProperties": false,
			"properties": {"path": {"type":"string"}},
			"required": ["path"],
			"type": "object"
		}`),
		Name:             NameRead,
		Label:            string(NameRead),
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         true,
	}
	cache := newSchemaValidatorCache()
	input := testArguments(`{"path":"README.md"}`)

	require.NoError(t, validateToolInput(&definition, input, cache))
	require.NoError(t, validateToolInput(&definition, input, cache))

	_, found, err := cache.validators.Get(schemaCacheKey(definition.Schema))
	require.NoError(t, err)
	assert.True(t, found)
}

func TestValidateToolInputNormalizesASTMode(t *testing.T) {
	t.Parallel()

	definition := Definition{
		Schema:           inputSchemaForName(NameAST),
		Name:             NameAST,
		Label:            string(NameAST),
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         true,
	}
	input := testArguments(`{"path":"main.go","mode":" SYMBOLS "}`)

	require.NoError(t, validateToolInput(&definition, input, nil))
}

func TestValidateToolInputRejectsInvalidSchemas(t *testing.T) {
	t.Parallel()

	definition := Definition{
		Schema:           testSchema(`{"type":"not-a-json-schema-type"}`),
		Name:             NameRead,
		Label:            string(NameRead),
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         true,
	}

	err := validateToolInput(&definition, EmptyArguments(), nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "compile tool input schema")
}

func TestExpectedToolInputShape(t *testing.T) {
	t.Parallel()

	assert.JSONEq(t, `{"path":"<path>","content":"<content>"}`, expectedToolInputShape([]string{
		toolInputPathKey,
		toolInputContentKey,
	}))
}

func testArguments(raw string) Arguments {
	arguments, err := ArgumentsFromRaw([]byte(raw))
	if err != nil {
		panic(err)
	}

	return arguments
}

func testSchema(raw string) Schema {
	schema, err := SchemaFromRaw([]byte(raw))
	if err != nil {
		panic(err)
	}

	return schema
}
