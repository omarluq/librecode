package tool

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const schemaRequiredKey = "required"

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
			name: "schema required string slice wins",
			definition: &Definition{
				Schema:           MustSchemaFromMap(map[string]any{schemaRequiredKey: []string{"alpha", "beta"}}),
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
				Schema: MustSchemaFromMap(map[string]any{
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
			name: "builtin fallback",
			definition: &Definition{
				Schema:           EmptySchema(),
				Name:             NameWrite,
				Label:            string(NameWrite),
				Description:      "",
				PromptSnippet:    "",
				PromptGuidelines: nil,
				ReadOnly:         false,
			},
			want: []string{toolInputPathKey, toolInputContentKey},
		},
		{
			name: "unknown tool has no required arguments",
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
			schema: MustSchemaFromMap(map[string]any{"type": "object"}),
			want:   nil,
			found:  false,
		},
		{
			name:   "invalid required",
			schema: MustSchemaFromMap(map[string]any{schemaRequiredKey: toolInputPathKey}),
			want:   nil,
			found:  false,
		},
		{
			name:   "string slice",
			schema: MustSchemaFromMap(map[string]any{schemaRequiredKey: []string{toolInputPathKey}}),
			want:   []string{toolInputPathKey},
			found:  true,
		},
		{
			name: "mixed required array filters invalid names",
			schema: MustSchemaFromMap(map[string]any{
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
		Schema: MustSchemaFromMap(map[string]any{
			schemaRequiredKey: []string{toolInputPathKey, toolInputContentKey},
		}),
		Name:             NameWrite,
		Label:            string(NameWrite),
		Description:      "",
		PromptSnippet:    "",
		PromptGuidelines: nil,
		ReadOnly:         false,
	}

	require.NoError(t, validateToolInput(&definition, map[string]any{toolInputPathKey: "x", toolInputContentKey: ""}))

	err := validateToolInput(&definition, map[string]any{toolInputContentKey: "x"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "write path is required")
	assert.Contains(t, err.Error(), `{"content":"\u003ccontent\u003e","path":"\u003cpath\u003e"}`)
}
