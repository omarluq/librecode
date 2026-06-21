package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	validationTestCompileSchemaErr = "compile tool input schema"
	validationTestValidateInputErr = "validate tool input"
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
			wantErrText: validationTestValidateInputErr,
		},
		{
			name:        "unknown property uses schema validation",
			inputJSON:   `{"path":"x","content":"","extra":true}`,
			wantErrText: validationTestValidateInputErr,
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

	tests := []struct {
		inputJSON   string
		name        string
		wantErrText string
	}{
		{name: "trims and lowercases mode", inputJSON: `{"path":"main.go","mode":" SYMBOLS "}`, wantErrText: ""},
		{name: "keeps already normalized mode", inputJSON: `{"path":"main.go","mode":"symbols"}`, wantErrText: ""},
		{
			name:        "ignores non-string mode before schema validation",
			inputJSON:   `{"path":"main.go","mode":42}`,
			wantErrText: validationTestValidateInputErr,
		},
		{name: "ignores missing mode", inputJSON: `{"path":"main.go"}`, wantErrText: ""},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			err := validateToolInput(&definition, testArguments(testCase.inputJSON), nil)
			if testCase.wantErrText == "" {
				require.NoError(t, err)

				return
			}

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErrText)
		})
	}
}

func TestValidateToolInputSchemaErrors(t *testing.T) {
	t.Parallel()

	tests := []struct { //nolint:govet // Table-driven tests prefer readable field order over fieldalignment.
		schema      Schema
		input       Arguments
		name        string
		wantErrText string
	}{
		{
			name:        "invalid schema type",
			schema:      testSchema(`{"type":"not-a-json-schema-type"}`),
			input:       EmptyArguments(),
			wantErrText: validationTestCompileSchemaErr,
		},
		{
			name:        "invalid schema JSON",
			schema:      Schema{raw: []byte(`{`)},
			input:       EmptyArguments(),
			wantErrText: validationTestCompileSchemaErr,
		},
		{
			name:        "unresolvable schema reference",
			schema:      testSchema(`{"$ref":"#/missing"}`),
			input:       EmptyArguments(),
			wantErrText: validationTestCompileSchemaErr,
		},
		{
			name:        "invalid input JSON",
			schema:      testSchema(`{"type":"object"}`),
			input:       Arguments{raw: []byte(`{`)},
			wantErrText: "decode tool input",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			definition := Definition{
				Schema:           testCase.schema,
				Name:             NameRead,
				Label:            string(NameRead),
				Description:      "",
				PromptSnippet:    "",
				PromptGuidelines: nil,
				ReadOnly:         true,
			}

			err := validateToolInput(&definition, testCase.input, nil)
			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.wantErrText)
		})
	}
}

func TestSchemaRequiredArgumentsRejectsInvalidRawSchema(t *testing.T) {
	t.Parallel()

	required, found := schemaRequiredArguments(Schema{raw: []byte(`{`)})

	assert.False(t, found)
	assert.Nil(t, required)
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

func TestCompiledToolInputSchemaWithCacheReturnsCompileError(t *testing.T) {
	t.Parallel()

	_, err := compiledToolInputSchema(testSchema(`{"type":"not-a-json-schema-type"}`), newSchemaValidatorCache())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "compile tool schema")
}

func TestNormalizeToolInputForValidationFallbacks(t *testing.T) {
	t.Parallel()

	nonASTDefinition := Definition{
		Schema:           EmptySchema(),
		Name:             NameRead,
		Label:            string(NameRead),
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         true,
	}
	astDefinition := Definition{
		Schema:           inputSchemaForName(NameAST),
		Name:             NameAST,
		Label:            string(NameAST),
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         true,
	}

	invalid := Arguments{raw: []byte(`{`)}
	assert.Equal(t, invalid.String(), normalizeToolInputForValidation(&nonASTDefinition, invalid).String())
	assert.Equal(t, invalid.String(), normalizeToolInputForValidation(&astDefinition, invalid).String())
}

func TestSchemaRequiredArgumentsReturnsNotFoundWhenAllNamesInvalid(t *testing.T) {
	t.Parallel()

	required, found := schemaRequiredArguments(testSchema(`{"required":["",42,false]}`))

	assert.False(t, found)
	assert.Nil(t, required)
}
