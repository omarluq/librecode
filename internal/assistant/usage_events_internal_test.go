package assistant

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
)

const historyUsageLabel = "history"

type recordingUsageExtensions struct {
	payload map[string]any
	testToolProvider
}

func (extensions *recordingUsageExtensions) Emit(_ context.Context, _ string, payload map[string]any) error {
	extensions.payload = payload

	return nil
}

func TestEmitUsageEventDetachesUsageBeforeCallback(t *testing.T) {
	t.Parallel()

	usage := &model.TokenUsage{
		Provenance: "",
		Breakdown:  map[string]int{historyUsageLabel: 10},
		TopContributors: []model.TokenContributor{
			{Label: historyUsageLabel, Role: "", Tokens: 10, Preview: "", Chars: 0},
		},
		ContextWindow: 0,
		ContextTokens: 0,
		InputTokens:   10,
		OutputTokens:  0,
	}

	var observed *model.TokenUsage

	extensions := new(recordingUsageExtensions)
	runtime := new(Runtime)
	runtime.extensions = extensions
	runtime.emitUsage(t.Context(), func(event StreamEvent) {
		observed = event.Usage
		event.Usage.InputTokens = 99
		event.Usage.Breakdown["history"] = 99
		event.Usage.TopContributors[0].Tokens = 99
	}, usage)

	require.NotNil(t, observed)
	assert.NotSame(t, usage, observed)
	assert.Equal(t, 10, usage.InputTokens)
	assert.Equal(t, 10, usage.Breakdown[historyUsageLabel])
	assert.Equal(t, 10, usage.TopContributors[0].Tokens)
	assert.Equal(t, 10, extensions.payload["input_tokens"])
	assert.Equal(t, map[string]any{historyUsageLabel: 10}, extensions.payload["breakdown"])

	contributors, ok := extensions.payload["topContributors"].([]any)
	require.True(t, ok)
	require.Len(t, contributors, 1)
	contributor, ok := contributors[0].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, 10, contributor["tokens"])
}
