package terminal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal"
)

func TestMergeTerminalUsageIgnoresInputOutputTokens(t *testing.T) {
	t.Parallel()

	current := model.TokenUsage{ContextWindow: 1_000_000, ContextTokens: 0, InputTokens: 0, OutputTokens: 0}
	next := model.TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: 12_000, OutputTokens: 700}

	assert.Equal(t, model.TokenUsage{
		ContextWindow: 1_000_000,
		ContextTokens: 0,
		InputTokens:   0,
		OutputTokens:  0,
	}, terminal.MergeTerminalUsageForTest(current, next))
}

func TestMergeTerminalUsagePreservesEstimatedContext(t *testing.T) {
	t.Parallel()

	current := model.TokenUsage{ContextWindow: 1_000_000, ContextTokens: 17_000, InputTokens: 17_000, OutputTokens: 0}
	next := model.TokenUsage{ContextWindow: 0, ContextTokens: 0, InputTokens: 12_000, OutputTokens: 700}

	assert.Equal(t, model.TokenUsage{
		ContextWindow: 1_000_000,
		ContextTokens: 17_000,
		InputTokens:   17_000,
		OutputTokens:  0,
	}, terminal.MergeTerminalUsageForTest(current, next))
}

func TestResetMessagesClearsTokenUsage(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()
	app.SetTokenUsageForTest(model.TokenUsage{ContextWindow: 1000, ContextTokens: 250, InputTokens: 0, OutputTokens: 0})

	app.ResetMessagesForTest()

	assert.Equal(t, model.EmptyTokenUsage(), app.TokenUsageForTest())
}

func TestTruncateMessagesClearsTokenUsage(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()
	app.SetTokenUsageForTest(model.TokenUsage{ContextWindow: 1000, ContextTokens: 250, InputTokens: 0, OutputTokens: 0})
	app.AddMessageForTest("user", "hello")

	app.TruncateMessagesForTest(0)

	assert.Equal(t, model.EmptyTokenUsage(), app.TokenUsageForTest())
}
