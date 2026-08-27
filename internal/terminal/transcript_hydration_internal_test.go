package terminal

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestHydrateOlderTranscriptPrependsPagesAndPreservesViewport(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "lazy transcript", "")
	require.NoError(t, err)

	var parentID *string
	for index := range 90 {
		entry, appendErr := app.runtime.SessionRepository().AppendMessage(
			ctx, session.ID, parentID, &database.MessageEntity{
				Timestamp: time.Date(2025, 1, 1, 0, 0, 0, 0, time.UTC),
				Role:      database.RoleAssistant,
				Content:   fmt.Sprintf("message %02d", index),
				Provider:  "",
				Model:     "",
				Parts:     nil,
			},
		)
		require.NoError(t, appendErr)

		parentID = &entry.ID
	}

	app.addWelcomeMessage()
	app.sessionID = session.ID
	require.NoError(t, app.loadInitialMessages(ctx))
	require.Len(t, app.transcript.History, defaultTerminalHeight)
	assert.Equal(t, "message 66", app.transcript.History[0].Content)
	assert.True(t, app.transcript.HasOlder)

	app.scrollOffset = keyboardScrollRows
	beforeOffset := app.scrollOffset
	require.NoError(t, app.hydrateOlderTranscript(ctx))
	assert.Equal(t, "message 02", app.transcript.History[0].Content)
	assert.True(t, app.transcript.HasOlder)
	assert.Greater(t, app.scrollOffset, beforeOffset)

	require.NoError(t, app.hydrateOlderTranscript(ctx))
	require.Len(t, app.transcript.History, 90)
	assert.Equal(t, "message 00", app.transcript.History[0].Content)
	assert.False(t, app.transcript.HasOlder)
}
