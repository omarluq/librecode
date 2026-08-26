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

	contributions, err := ContributionsFromPayloadWithLimit(map[string]any{
		payloadContributionsKey: []any{
			map[string]any{
				jsonToolNameKey: "note",
				jsonContentKey:  " remember this ",
				"metadata": map[string]any{
					"reason": "test",
				},
			},
		},
	}, ContributionAggregateMaxTokens)

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

	contributions, err := ContributionsFromPayloadWithLimit(map[string]any{
		payloadContributionsKey: map[string]any{
			"1": map[string]any{jsonContentKey: "first"},
			"2": map[string]any{jsonContentKey: "second"},
		},
	}, ContributionAggregateMaxTokens)

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

			_, err := ContributionsFromPayloadWithLimit(testCase.payload, ContributionAggregateMaxTokens)
			require.Error(t, err)
		})
	}
}

func TestContributionsFromPayloadEnforcesAggregateLimitInOrder(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		contents    []string
		wantTokens  []int
		wantErrText []string
		limit       int
	}{
		{
			name:        "exact limit accepted",
			contents:    []string{strings.Repeat("a", 8), strings.Repeat("b", 12)},
			wantTokens:  []int{2, 3},
			wantErrText: nil,
			limit:       5,
		},
		{
			name:       "first contribution over limit rejected",
			wantTokens: nil,
			contents:   []string{strings.Repeat("secret", 8)},
			limit:      5,
			wantErrText: []string{
				"context contribution 0", "requires 12 tokens", "after 0 used", "limit is 5 tokens",
			},
		},
		{
			name:       "later contribution over limit rejected deterministically",
			wantTokens: nil,
			contents:   []string{strings.Repeat("a", 8), strings.Repeat("secret", 4), strings.Repeat("z", 4)},
			limit:      5,
			wantErrText: []string{
				"context contribution 1", "requires 6 tokens", "after 2 used", "limit is 5 tokens",
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			raw := make([]any, 0, len(testCase.contents))
			for index, content := range testCase.contents {
				raw = append(raw, map[string]any{
					jsonToolNameKey: "item-" + string(rune('0'+index)),
					jsonContentKey:  content,
				})
			}

			contributions, err := ContributionsFromPayloadWithLimit(
				map[string]any{payloadContributionsKey: raw},
				testCase.limit,
			)
			if len(testCase.wantErrText) == 0 {
				require.NoError(t, err)
				require.Len(t, contributions, len(testCase.wantTokens))

				for index, tokens := range testCase.wantTokens {
					assert.Equal(t, tokens, contributions[index].Tokens)
				}

				return
			}

			require.Error(t, err)
			assert.Nil(t, contributions, "aggregate rejection must not return a partial list")

			for _, text := range testCase.wantErrText {
				assert.Contains(t, err.Error(), text)
			}

			assert.NotContains(t, err.Error(), "secretsecret", "diagnostics must not expose content")
		})
	}
}

func TestContributionsFromPayloadRejectsInvalidAggregateLimit(t *testing.T) {
	t.Parallel()

	contributions, err := ContributionsFromPayloadWithLimit(map[string]any{}, 0)

	require.Error(t, err)
	assert.Nil(t, contributions)
	assert.Contains(t, err.Error(), "aggregate token limit must be positive")
}

func TestAppendContributionsAddsExtensionContextBlocks(t *testing.T) {
	t.Parallel()

	result := &BuildResult{ExtensionContributionTokenLimit: 0,
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
