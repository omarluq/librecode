package terminal

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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
	app.sessionID = "displayed-session"
	called := false

	applied := app.withSessionView("missing-session", func() {
		called = true

		app.addSystemMessage("misrouted event")
	})

	assert.False(t, applied)
	assert.False(t, called)
	assert.Equal(t, "displayed-session", app.sessionID)
	assert.Empty(t, app.transcript.History)
}
