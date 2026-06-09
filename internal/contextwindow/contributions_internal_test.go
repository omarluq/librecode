package contextwindow

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
)

func TestContributionsFromPayloadParsesListAndDefaults(t *testing.T) {
	t.Parallel()

	contributions, err := ContributionsFromPayload(map[string]any{
		payloadContributionsKey: []any{
			map[string]any{
				jsonToolNameKey: "note",
				jsonContentKey:  " remember this ",
				"metadata": map[string]any{
					"reason": "test",
				},
			},
		},
	})

	require.NoError(t, err)
	require.Len(t, contributions, 1)
	assert.Equal(t, "note", contributions[0].Name)
	assert.Equal(t, "remember this", contributions[0].Content)
	assert.Equal(t, ContributionSourceExtension, contributions[0].Source)
	assert.Equal(t, ContributionRoleSystem, contributions[0].Role)
	assert.Equal(t, "test", contributions[0].Metadata["reason"])
	assert.Positive(t, contributions[0].Tokens)
}

func TestContributionsFromPayloadParsesLuaNumericMap(t *testing.T) {
	t.Parallel()

	contributions, err := ContributionsFromPayload(map[string]any{
		payloadContributionsKey: map[string]any{
			"1": map[string]any{jsonContentKey: "first"},
			"2": map[string]any{jsonContentKey: "second"},
		},
	})

	require.NoError(t, err)
	require.Len(t, contributions, 2)
	assert.Equal(t, "first", contributions[0].Content)
	assert.Equal(t, "second", contributions[1].Content)
}

func TestContributionsFromPayloadRejectsInvalidShapes(t *testing.T) {
	t.Parallel()

	tests := []struct {
		payload map[string]any
		name    string
	}{
		{name: "scalar contributions", payload: map[string]any{payloadContributionsKey: "bad"}},
		{name: "non object contribution", payload: map[string]any{payloadContributionsKey: []any{"bad"}}},
		{
			name: "blank content",
			payload: map[string]any{
				payloadContributionsKey: []any{map[string]any{jsonContentKey: " "}},
			},
		},
		{
			name: "oversized content",
			payload: map[string]any{
				payloadContributionsKey: []any{
					map[string]any{jsonContentKey: strings.Repeat("token ", 9000)},
				},
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			_, err := ContributionsFromPayload(testCase.payload)
			require.Error(t, err)
		})
	}
}

func TestAppendContributionsAddsExtensionContextBlocks(t *testing.T) {
	t.Parallel()

	result := &BuildResult{
		Breakdown:     nil,
		SystemPrompt:  "base",
		Contributions: []Contribution{},
		Messages:      nil,
		UsageAnchor:   nil,
		Usage:         model.EmptyTokenUsage(),
	}
	AppendContributions(result, []Contribution{{
		Metadata: nil,
		Source:   "ext",
		Name:     "note",
		Role:     "system",
		Content:  "content",
		Tokens:   2,
	}})

	require.Len(t, result.Contributions, 1)
	assert.Contains(t, result.SystemPrompt, "<extension_context>")
	assert.Contains(t, result.SystemPrompt, `name="note"`)
	assert.Contains(t, result.SystemPrompt, `source="ext"`)
	assert.Contains(t, result.SystemPrompt, "content")
}
