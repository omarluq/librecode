package tool

import (
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	schemaRequiredKey   = "required"
	schemaTypeKey       = "type"
	schemaObjectType    = "object"
	schemaStringType    = "string"
	schemaPropertiesKey = "properties"
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
				Schema:           testSchema(map[string]any{schemaRequiredKey: []string{"alpha", "beta"}}),
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
				Schema: testSchema(map[string]any{
					schemaRequiredKey: []any{toolInputPathKey, " ", 42, "limit"},
				}),
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
			schema: testSchema(map[string]any{schemaTypeKey: schemaObjectType}),
			want:   nil,
			found:  false,
		},
		{
			name:   "invalid required",
			schema: testSchema(map[string]any{schemaRequiredKey: toolInputPathKey}),
			want:   nil,
			found:  false,
		},
		{
			name:   "string slice",
			schema: testSchema(map[string]any{schemaRequiredKey: []string{toolInputPathKey}}),
			want:   []string{toolInputPathKey},
			found:  true,
		},
		{
			name: "mixed required array filters invalid names",
			schema: testSchema(map[string]any{
				schemaRequiredKey: []any{toolInputPathKey, "", false, toolInputContentKey},
			}),
			want:  []string{toolInputPathKey, toolInputContentKey},
			found: true,
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

	definition := Definition{
		Schema: testSchema(map[string]any{
			"additionalProperties": false,
			schemaPropertiesKey: map[string]any{
				toolInputPathKey:    map[string]any{schemaTypeKey: schemaStringType},
				toolInputContentKey: map[string]any{schemaTypeKey: schemaStringType},
			},
			schemaRequiredKey: []string{toolInputPathKey, toolInputContentKey},
			schemaTypeKey:     schemaObjectType,
		}),
		Name:             NameWrite,
		Label:            string(NameWrite),
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         false,
	}

	tests := []struct {
		input       map[string]any
		name        string
		wantErrText string
	}{
		{
			name:        "valid input",
			input:       map[string]any{toolInputPathKey: "x", toolInputContentKey: ""},
			wantErrText: "",
		},
		{
			name:        "missing required keeps user friendly error",
			input:       map[string]any{toolInputContentKey: "x"},
			wantErrText: "write path is required",
		},
		{
			name:        "invalid type uses schema validation",
			input:       map[string]any{toolInputPathKey: 42, toolInputContentKey: "x"},
			wantErrText: "validate tool input",
		},
		{
			name:        "unknown property uses schema validation",
			input:       map[string]any{toolInputPathKey: "x", toolInputContentKey: "", "extra": true},
			wantErrText: "validate tool input",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			input := testArguments(testCase.input)

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

func testArguments(value map[string]any) Arguments {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	arguments, err := ArgumentsFromRaw(encoded)
	if err != nil {
		panic(err)
	}

	return arguments
}

func testSchema(value map[string]any) Schema {
	encoded, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}

	schema, err := SchemaFromRaw(encoded)
	if err != nil {
		panic(err)
	}

	return schema
}
