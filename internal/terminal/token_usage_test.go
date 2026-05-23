package terminal_test

import (
	"testing"

	"github.com/stretchr/testify/assert"

	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/terminal"
)

const (
	testBreakdownExtensions = "extensions"
	testBreakdownHistory    = "history"
	testBreakdownSystem     = "system"
)

func TestMergeTerminalUsageIgnoresInputOutputTokens(t *testing.T) {
	t.Parallel()

	current := model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 1_000_000,
		ContextTokens: 0,
		InputTokens:   0,
		OutputTokens:  0,
	}
	next := model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 0,
		ContextTokens: 0,
		InputTokens:   12_000,
		OutputTokens:  700,
	}

	assert.Equal(t, model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 1_000_000,
		ContextTokens: 0,
		InputTokens:   0,
		OutputTokens:  0,
	}, terminal.MergeTerminalUsageForTest(current, next))
}

func TestMergeTerminalUsagePreservesEstimatedContext(t *testing.T) {
	t.Parallel()

	current := model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 1_000_000,
		ContextTokens: 17_000,
		InputTokens:   17_000,
		OutputTokens:  0,
	}
	next := model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 0,
		ContextTokens: 0,
		InputTokens:   12_000,
		OutputTokens:  700,
	}

	assert.Equal(t, model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 1_000_000,
		ContextTokens: 17_000,
		InputTokens:   17_000,
		OutputTokens:  0,
	}, terminal.MergeTerminalUsageForTest(current, next))
}

func TestResetMessagesClearsTokenUsage(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()
	app.SetTokenUsageForTest(model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 1000,
		ContextTokens: 250,
		InputTokens:   0,
		OutputTokens:  0,
	})

	app.ResetMessagesForTest()

	assert.Equal(t, model.EmptyTokenUsage(), app.TokenUsageForTest())
}

func TestTruncateMessagesClearsTokenUsage(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()
	app.SetTokenUsageForTest(model.TokenUsage{
		Breakdown:     nil,
		ContextWindow: 1000,
		ContextTokens: 250,
		InputTokens:   0,
		OutputTokens:  0,
	})
	app.AddMessageForTest("user", "hello")

	app.TruncateMessagesForTest(0)

	assert.Equal(t, model.EmptyTokenUsage(), app.TokenUsageForTest())
}

func TestMergeTerminalUsageKeepsBreakdown(t *testing.T) {
	t.Parallel()

	current := model.TokenUsage{
		Breakdown:     map[string]int{testBreakdownSystem: 10},
		ContextWindow: 1000,
		ContextTokens: 20,
		InputTokens:   20,
		OutputTokens:  0,
	}
	next := model.TokenUsage{
		Breakdown: map[string]int{
			testBreakdownExtensions: 5,
			testBreakdownHistory:    15,
			testBreakdownSystem:     10,
		},
		ContextWindow: 0,
		ContextTokens: 30,
		InputTokens:   30,
		OutputTokens:  0,
	}

	merged := terminal.MergeTerminalUsageForTest(current, next)

	assert.Equal(t, map[string]int{
		testBreakdownExtensions: 5,
		testBreakdownHistory:    15,
		testBreakdownSystem:     10,
	}, merged.Breakdown)
}

func TestContextBreakdownLinesSortsAndSkipsEmptyValues(t *testing.T) {
	t.Parallel()

	lines := terminal.ContextBreakdownLinesForTest(map[string]int{
		testBreakdownExtensions: 0,
		testBreakdownHistory:    1200,
		testBreakdownSystem:     50,
	})

	assert.Equal(t, []string{"  - history: 1.2k", "  - system: 50"}, lines)
}
