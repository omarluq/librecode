package terminal

import (
	"testing"

	"github.com/omarluq/librecode/internal/transcript"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/model"
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
	}, mergeTerminalUsage(current, next))
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
	}, mergeTerminalUsage(current, next))
}

func TestApplyTokenUsageSnapshotAllowsContextDecrease(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	app.applyTokenUsage(&model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   80_000,
		InputTokens:     80_000,
		OutputTokens:    0,
	})
	usage := model.TokenUsage{
		Breakdown: map[string]int{testBreakdownHistory: 5_000},
		TopContributors: []model.TokenContributor{
			{Label: terminalTestSummary, Role: "", Preview: "", Tokens: 5_000, Chars: 20_000},
		},
		ContextWindow: 100_000,
		ContextTokens: 12_000,
		InputTokens:   12_000,
		OutputTokens:  0,
	}

	app.applyTokenUsageSnapshotForTest(&usage)
	usage.Breakdown[testBreakdownHistory] = 99
	usage.TopContributors[0].Label = "mutated"

	assert.Equal(t, model.TokenUsage{
		Breakdown: map[string]int{testBreakdownHistory: 5_000},
		TopContributors: []model.TokenContributor{
			{Label: terminalTestSummary, Role: "", Preview: "", Tokens: 5_000, Chars: 20_000},
		},
		ContextWindow: 100_000,
		ContextTokens: 12_000,
		InputTokens:   12_000,
		OutputTokens:  0,
	}, app.tokenUsage)
}

func TestFormatTokenStatusHidesWindowOnlyUsage(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   9_000,
		ContextTokens:   0,
		InputTokens:     0,
		OutputTokens:    0,
	}

	assert.Empty(t, formatTokenStatus(usage))
}

func TestResetMessagesClearsTokenUsage(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	app.setTokenUsageForTest(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1000,
		ContextTokens:   250,
		InputTokens:     0,
		OutputTokens:    0,
	})

	app.resetMessages()

	assert.Equal(t, model.EmptyTokenUsage(), app.tokenUsage)
}

func TestTruncateMessagesClearsTokenUsage(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	app.setTokenUsageForTest(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   1000,
		ContextTokens:   250,
		InputTokens:     0,
		OutputTokens:    0,
	})
	app.addMessageForTest("user", "hello")

	app.truncateMessages(0)

	assert.Equal(t, model.EmptyTokenUsage(), app.tokenUsage)
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
			{Label: "message 1", Role: "user", Preview: terminalTestGreeting, Tokens: 30, Chars: 120},
		},
		ContextWindow: 0,
		ContextTokens: 30,
		InputTokens:   30,
		OutputTokens:  0,
	}

	merged := mergeTerminalUsage(current, next)
	next.TopContributors[0].Label = "mutated"

	assert.Equal(t, map[string]int{
		testBreakdownExtensions: 5,
		testBreakdownHistory:    15,
		testBreakdownSystem:     10,
	}, merged.Breakdown)
	assert.Equal(t, "message 1", merged.TopContributors[0].Label)
}

func TestContextBreakdownLinesSortsAndSkipsEmptyValues(t *testing.T) {
	t.Parallel()

	lines := contextBreakdownLines(map[string]int{
		testBreakdownExtensions: 0,
		testBreakdownHistory:    1200,
		testBreakdownSystem:     50,
	})

	assert.Equal(t, []string{"  - history: 1.2k", "  - system: 50"}, lines)
}

func TestContextContributorLinesFormatsTopContributors(t *testing.T) {
	t.Parallel()

	lines := contextContributorLines([]model.TokenContributor{
		{Label: "message 1", Role: "user", Preview: "long pasted traceback", Tokens: 18000, Chars: 72000},
	})

	assert.Equal(t, []string{"  - message 1 18k user — long pasted traceback"}, lines)
}

func TestCompactCountFormatsMillionValues(t *testing.T) {
	t.Parallel()

	assert.Equal(t, "ctx 1.2m", formatContextUsage(model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   0,
		ContextTokens:   1_250_000,
		InputTokens:     0,
		OutputTokens:    0,
	}))
}

func TestShowContextInfoHandlesContextCommandWithoutUsage(t *testing.T) {
	t.Parallel()

	app := newTestApp()

	require.NoError(t, app.showContextInfo(t.Context(), "/context"))

	messages := app.messageContentsForTest()
	require.NotEmpty(t, messages)
	assert.Equal(t, "context:", messages[len(messages)-1])
}

func TestShowContextInfoDisplaysSummaryAndBreakdown(t *testing.T) {
	t.Parallel()

	app := newTestApp()
	app.setTokenUsageForTest(model.TokenUsage{
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

	require.NoError(t, app.showContextInfo(t.Context(), "/context now"))

	messages := app.messageContentsForTest()
	require.NotEmpty(t, messages)
	message := messages[len(messages)-1]
	assert.Contains(t, message, "context:")
	assert.Contains(t, message, "- ctx 250/1.0k 25%")
	assert.Contains(t, message, "- breakdown:\n  - history: 1.2k\n  - system: 50")
	assert.Contains(t, message, "- top contributors:")
	assert.Contains(t, message, "message 2 7.0k assistant")
}

func TestFormatContextUsageUsesModelWindow(t *testing.T) {
	t.Parallel()

	usage := model.TokenUsage{
		Breakdown: map[string]int{
			"usable_input": 132_624,
		},
		TopContributors: nil,
		ContextWindow:   272_000,
		ContextTokens:   156_652,
		InputTokens:     156_652,
		OutputTokens:    0,
	}

	assert.Equal(t, "ctx 156k/272k 57%", formatContextUsage(usage))
}

func newTestApp() *App {
	return newApp(nil, &RunOptions{
		Extensions: nil,
		Resources:  nil,
		Runtime:    nil,
		Settings:   nil,
		Models:     nil,
		Auth:       nil,
		Config:     nil,
		CWD:        "",
		SessionID:  "",
	})
}

func (app *App) setTokenUsageForTest(usage model.TokenUsage) {
	app.tokenUsage = usage
}

func (app *App) applyTokenUsageSnapshotForTest(usage *model.TokenUsage) {
	app.applyTokenUsageEvent(usage, true)
}

func (app *App) addMessageForTest(role, content string) {
	app.addMessage(transcript.Role(role), content)
}

func (app *App) messageContentsForTest() []string {
	contents := make([]string, 0, len(app.transcript.History))
	for _, message := range app.transcript.History {
		contents = append(contents, message.Content)
	}

	return contents
}
