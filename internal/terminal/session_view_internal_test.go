package terminal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/tool"
	"github.com/omarluq/librecode/internal/transcript"
)

func TestPromptUserEntryAssignsDisplayedNewSession(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.ID = 7
	app.working = true

	app.handlePromptAsyncEvent(t.Context(), asyncTestEvent(
		asyncEventPromptUserEntry,
		"created-session",
		"user-entry",
		7,
	))
	app.handlePromptAsyncEvent(t.Context(), asyncTestEvent(
		asyncEventPromptDelta,
		"",
		"streamed response",
		7,
	))

	require.NotNil(t, app.activePrompt)
	assert.Equal(t, "created-session", app.sessionID)
	assert.Equal(t, "created-session", app.activePrompt.SessionID)
	require.Len(t, app.transcript.Streaming.Blocks, 1)
	assert.Equal(t, "streamed response", app.transcript.Streaming.Blocks[0].Content)
	assert.False(t, app.inspectingWhilePromptRuns())
}

func TestWithSessionViewRejectsEventWhenOwnerViewIsMissing(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = benchmarkDisplayedSession
	called := false

	applied := app.withSessionView("missing-session", func() {
		called = true

		app.addSystemMessage("misrouted event")
	})

	assert.False(t, applied)
	assert.False(t, called)
	assert.Equal(t, benchmarkDisplayedSession, app.sessionID)
	assert.Empty(t, app.transcript.History)
}

func TestSessionViewSaveAndRestoreClonePromptHistory(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	const savedPrompt = "saved prompt"

	app.sessionID = benchmarkDisplayedSession
	app.promptHistory = []string{savedPrompt}
	app.saveSessionView()

	app.promptHistory[0] = "mutated active prompt"
	assert.Equal(t, []string{savedPrompt}, app.sessionViews[benchmarkDisplayedSession].promptHistory)

	require.True(t, app.restoreSessionView(benchmarkDisplayedSession))
	app.promptHistory[0] = "changed restored prompt"
	assert.Equal(t, []string{savedPrompt}, app.sessionViews[benchmarkDisplayedSession].promptHistory)
}

func TestSessionViewSaveAndRestoreCloneMutableMessageState(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = benchmarkDisplayedSession
	app.appendMessage(newChatMessage(transcript.RoleAssistant, "history"))
	app.appendStreamingBlock(transcript.RoleAssistant, "streaming")
	app.runningToolBlocks = []runningToolBlock{{
		StartedAt: time.Now(), Call: assistant.ToolCallEvent{
			ArgumentsJSON: "{}", ID: "tool", ParentCallID: "", Name: "bash",
			Arguments: tool.EmptyArguments(), Sequence: 0,
		},
	}}
	app.liveAgentCompletions = []chatMessage{newChatMessage(transcript.RoleAssistant, "completion")}
	app.steeringMessages = promptDrafts("steering")
	app.saveSessionView()

	app.transcript.History[0].Content = "mutated history"
	app.transcript.Streaming.Blocks[0].Content = "mutated streaming"
	app.runningToolBlocks[0].Call.ID = "mutated tool"
	app.liveAgentCompletions[0].Content = "mutated completion"
	app.steeringMessages[0].Text = "mutated steering"
	cached := app.sessionViews[benchmarkDisplayedSession]
	assert.Equal(t, "history", cached.transcript.History[0].Content)
	assert.Equal(t, "streaming", cached.transcript.Streaming.Blocks[0].Content)
	assert.Equal(t, "tool", cached.runningToolBlocks[0].Call.ID)
	assert.Equal(t, "completion", cached.liveAgentCompletions[0].Content)
	assert.Equal(t, "steering", cached.steeringMessages[0].Text)

	require.True(t, app.restoreSessionView(benchmarkDisplayedSession))
	app.transcript.History[0].Content = "restored history mutation"
	app.transcript.Streaming.Blocks[0].Content = "restored streaming mutation"
	app.runningToolBlocks[0].Call.ID = "restored tool mutation"
	app.liveAgentCompletions[0].Content = "restored completion mutation"
	app.steeringMessages[0].Text = "restored steering mutation"
	cached = app.sessionViews[benchmarkDisplayedSession]
	assert.Equal(t, "history", cached.transcript.History[0].Content)
	assert.Equal(t, "streaming", cached.transcript.Streaming.Blocks[0].Content)
	assert.Equal(t, "tool", cached.runningToolBlocks[0].Call.ID)
	assert.Equal(t, "completion", cached.liveAgentCompletions[0].Content)
	assert.Equal(t, "steering", cached.steeringMessages[0].Text)
}

func TestPromptEventReportsMissingOwnerView(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.sessionID = benchmarkDisplayedSession
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.SessionID = "missing-session"

	app.handlePromptAsyncEvent(t.Context(), asyncTestEvent(
		asyncEventPromptDelta,
		"dropped delta",
		"",
		app.activePrompt.ID,
	))

	assert.Equal(t, "prompt event owner view is unavailable", app.statusMessage)
	assert.Empty(t, app.transcript.Streaming.Blocks)
}
