package terminal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal"
)

const testContextContributorRole = "person"

func TestContextContributorLinesSkipEmptyAndTokenlessEntries(t *testing.T) {
	t.Parallel()

	lines := terminal.ContextContributorLinesForTest([]model.TokenContributor{
		{Label: "empty", Role: testContextContributorRole, Preview: "ignored", Tokens: 0, Chars: 0},
		{Label: "message", Role: "", Preview: "", Tokens: 42, Chars: 168},
	})

	assert.Equal(t, []string{"  - message 42"}, lines)
}

func TestApplyTokenUsageAndFormattingVariants(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()
	app.ApplyTokenUsageForTest(nil)
	assert.Equal(t, model.EmptyTokenUsage(), app.TokenUsageForTest())
	assert.Empty(t, app.TokenStatusTextForTest())

	usage := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   9000,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}
	app.ApplyTokenUsageForTest(&usage)
	assert.Empty(t, app.TokenStatusTextForTest())
	assert.Empty(t, terminal.FormatTokenStatusForTest(usage))

	assert.Equal(t, "ctx 42", terminal.FormatContextUsageForTest(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   42,
		InputTokens:     0,
		OutputTokens:    0,
	}))
	assert.Empty(t, terminal.FormatTokenStatusForTest(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     7,
		OutputTokens:    0,
	}))
}
