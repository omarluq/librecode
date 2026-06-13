package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
)

const testContextContributorRole = "person"

func TestContextContributorLinesSkipEmptyAndTokenlessEntries(t *testing.T) {
	t.Parallel()

	lines := contextContributorLines([]model.TokenContributor{
		{Label: "empty", Role: testContextContributorRole, Preview: "ignored", Tokens: 0, Chars: 0},
		{Label: "message", Role: "", Preview: "", Tokens: 42, Chars: 168},
	})

	assert.Equal(t, []string{"  - message 42"}, lines)
}

func TestApplyTokenUsageAndFormattingVariants(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	app.applyTokenUsage(nil)
	assert.Equal(t, model.EmptyTokenUsage(), app.tokenUsage)
	assert.Empty(t, app.tokenStatusText())

	usage := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   9000,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}
	app.applyTokenUsage(&usage)
	assert.Empty(t, app.tokenStatusText())
	assert.Empty(t, formatTokenStatus(usage))

	assert.Equal(t, "ctx 42", formatContextUsage(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   42,
		InputTokens:     0,
		OutputTokens:    0,
	}))
	assert.Empty(t, formatTokenStatus(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     7,
		OutputTokens:    0,
	}))
}
