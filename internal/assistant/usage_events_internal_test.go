package assistant

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
)

func TestEmitUsageEventDetachesUsageBeforeCallback(t *testing.T) {
	t.Parallel()

	usage := &model.TokenUsage{
		Provenance:      "",
		Breakdown:       map[string]int{"history": 10},
		TopContributors: []model.TokenContributor{{Label: "history", Role: "", Tokens: 10, Preview: "", Chars: 0}},
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     10,
		OutputTokens:    0,
	}

	var observed *model.TokenUsage

	new(Runtime).emitUsage(t.Context(), func(event StreamEvent) {
		observed = event.Usage
		event.Usage.InputTokens = 99
		event.Usage.Breakdown["history"] = 99
		event.Usage.TopContributors[0].Tokens = 99
	}, usage)

	require.NotNil(t, observed)
	assert.NotSame(t, usage, observed)
	assert.Equal(t, 10, usage.InputTokens)
	assert.Equal(t, 10, usage.Breakdown["history"])
	assert.Equal(t, 10, usage.TopContributors[0].Tokens)
}
