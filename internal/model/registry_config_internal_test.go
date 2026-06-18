package model

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseCustomModelsUsesHuJSON(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		input  string
		assert func(t *testing.T, result customModelsResult)
	}{
		{
			name: "accepts comments trailing commas and strings containing slashes",
			input: `{
				// line comments are valid in models.json
				"providers": {
					"custom": {
						"name": "Custom",
						"base_url": "https://example.test/v1",
						"headers": {
							"x-test": "// preserved inside strings",
						},
						"models": [
							{
								"id": "custom-model",
								"name": "Custom Model",
								"input": ["text", "image"],
								"context_window": 123,
							},
						],
					},
				},
			}`,
			assert: func(t *testing.T, result customModelsResult) {
				t.Helper()

				require.NoError(t, result.Err)
				require.Len(t, result.Models, 1)
				assert.Equal(t, "custom", result.Models[0].Provider)
				assert.Equal(t, "custom-model", result.Models[0].ID)
				assert.Equal(t, []InputMode{InputText, InputImage}, result.Models[0].Input)
				assert.Equal(t, 123, result.Models[0].ContextWindow)
				assert.Equal(t, "https://example.test/v1", result.ProviderPatches["custom"].BaseURL)
				assert.Equal(
					t,
					map[string]string{"x-test": "// preserved inside strings"},
					result.ProviderConfigs["custom"].Headers,
				)
			},
		},
		{
			name:  "rejects invalid hujson",
			input: `{"providers": { /* unclosed comment `,
			assert: func(t *testing.T, result customModelsResult) {
				t.Helper()

				require.Error(t, result.Err)
				assert.Empty(t, result.Models)
				assert.Empty(t, result.ProviderConfigs)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			result := parseCustomModels([]byte(tt.input), "models.json")

			tt.assert(t, result)
		})
	}
}
