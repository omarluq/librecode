package terminal_test

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

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
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1_000_000,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}
	next := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     12_000,
		OutputTokens:    700,
	}

	assert.Equal(t, model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1_000_000,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}, terminal.MergeTerminalUsageForTest(current, next))
}

func TestMergeTerminalUsagePreservesEstimatedContext(t *testing.T) {
	t.Parallel()

	current := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1_000_000,
		ContextTokens:   17_000,
		InputTokens:     17_000,
		OutputTokens:    0,
	}
	next := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   0,
		InputTokens:     12_000,
		OutputTokens:    700,
	}

	assert.Equal(t, model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1_000_000,
		ContextTokens:   17_000,
		InputTokens:     17_000,
		OutputTokens:    0,
	}, terminal.MergeTerminalUsageForTest(current, next))
}

func TestResetMessagesClearsTokenUsage(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()
	app.SetTokenUsageForTest(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1000,
		ContextTokens:   250,
		InputTokens:     0,
		OutputTokens:    0,
	})

	app.ResetMessagesForTest()

	assert.Equal(t, model.EmptyTokenUsage(), app.TokenUsageForTest())
}

func TestTruncateMessagesClearsTokenUsage(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()
	app.SetTokenUsageForTest(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1000,
		ContextTokens:   250,
		InputTokens:     0,
		OutputTokens:    0,
	})
	app.AddMessageForTest("user", "hello")

	app.TruncateMessagesForTest(0)

	assert.Equal(t, model.EmptyTokenUsage(), app.TokenUsageForTest())
}

func TestMergeTerminalUsageKeepsBreakdownAndContributors(t *testing.T) {
	t.Parallel()

	current := model.TokenUsage{
		Breakdown:       map[string]int{testBreakdownSystem: 10},
		TopContributors: nil,
		ContextWindow:   1000,
		ContextTokens:   20,
		InputTokens:     20,
		OutputTokens:    0,
	}
	next := model.TokenUsage{
		Breakdown: map[string]int{
			testBreakdownExtensions: 5,
			testBreakdownHistory:    15,
			testBreakdownSystem:     10,
		},
		TopContributors: []model.TokenContributor{
			{Label: "message 1", Role: "user", Preview: "hello", Tokens: 30, Chars: 120},
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
	assert.Equal(t, next.TopContributors, merged.TopContributors)
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

func TestContextContributorLinesFormatsTopContributors(t *testing.T) {
	t.Parallel()

	lines := terminal.ContextContributorLinesForTest([]model.TokenContributor{
		{Label: "message 1", Role: "user", Preview: "long pasted traceback", Tokens: 18000, Chars: 72000},
	})

	assert.Equal(t, []string{"  - message 1 18k user — long pasted traceback"}, lines)
}

func TestShowContextInfoHandlesContextCommandWithoutUsage(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()

	require.NoError(t, app.ShowContextInfoForTest("/context"))

	messages := app.MessageContentsForTest()
	require.NotEmpty(t, messages)
	assert.Equal(t, "context:", messages[len(messages)-1])
}

func TestShowContextInfoDisplaysSummaryAndBreakdown(t *testing.T) {
	t.Parallel()

	app := terminal.NewAppForTest()
	app.SetTokenUsageForTest(model.TokenUsage{
		Breakdown: map[string]int{
			testBreakdownExtensions: 0,
			testBreakdownHistory:    1200,
			testBreakdownSystem:     50,
		},
		TopContributors: []model.TokenContributor{
			{Label: "message 2", Role: "assistant", Preview: "architecture summary", Tokens: 7000, Chars: 28000},
		},
		ContextWindow: 1000,
		ContextTokens: 250,
		InputTokens:   0,
		OutputTokens:  0,
	})

	require.NoError(t, app.ShowContextInfoForTest("/context now"))

	messages := app.MessageContentsForTest()
	require.NotEmpty(t, messages)
	message := messages[len(messages)-1]
	assert.Contains(t, message, "context:")
	assert.Contains(t, message, "- ctx 250/1.0k 25%")
	assert.True(t, strings.Contains(message, "- breakdown:\n  - history: 1.2k\n  - system: 50"))
	assert.Contains(t, message, "- top contributors:")
	assert.Contains(t, message, "message 2 7.0k assistant")
}
