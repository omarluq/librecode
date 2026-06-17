package terminal

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const sessionCommandsParentID = "parent"

func TestCloneSessionRequiresActiveSession(t *testing.T) {
	t.Parallel()

	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))

	err := app.cloneSession(t.Context(), "copy")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no active session")
}

func TestCloneSessionCreatesChildSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "source", "")
	require.NoError(t, err)

	parentID := sessionCommandsParentID
	app.sessionID = session.ID
	app.pendingParentID = &parentID

	require.NoError(t, app.cloneSession(ctx, "copy"))

	assert.NotEqual(t, session.ID, app.sessionID)
	assert.Nil(t, app.pendingParentID)
	cloned, found, err := app.runtime.SessionRepository().GetSession(ctx, app.sessionID)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "copy", cloned.Name)
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, app.sessionID)
}

func TestLastAssistantMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))

	message, found, err := app.lastAssistantMessage(ctx)
	require.NoError(t, err)
	assert.False(t, found)
	assert.Nil(t, message)

	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "copy", "")
	require.NoError(t, err)

	app.sessionID = session.ID

	appendSessionMessage(t, app, session.ID, database.RoleAssistant, "first")
	appendSessionMessage(t, app, session.ID, database.RoleUser, "question")
	appendSessionMessage(t, app, session.ID, database.RoleAssistant, "   ")
	appendSessionMessage(t, app, session.ID, database.RoleAssistant, "second")

	message, found, err = app.lastAssistantMessage(ctx)
	require.NoError(t, err)
	require.True(t, found)
	assert.Equal(t, "second", message.Content)
}

func TestCopyLastAssistantMessage(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	clipboard := newClipboardScreen()
	systemClipboard := newFakeSystemClipboard()
	expectClipboardWrite(t, systemClipboard, "answer")

	app.screen = clipboard
	app.systemClipboard = systemClipboard
	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "copy", "")
	require.NoError(t, err)

	app.sessionID = session.ID

	appendSessionMessage(t, app, session.ID, database.RoleAssistant, "answer")

	require.NoError(t, app.copyLastAssistantMessage(ctx))

	assert.Equal(t, "answer", string(clipboard.clipboard))
	assertClipboardExpectations(t, systemClipboard)
	assert.Equal(t, "copied last assistant message", app.statusMessage)
}

func TestCopyLastAssistantMessageRequiresAssistantMessage(t *testing.T) {
	t.Parallel()

	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))

	err := app.copyLastAssistantMessage(t.Context())

	require.Error(t, err)
	assert.Contains(t, err.Error(), "no assistant message to copy")
}

func appendSessionMessage(t *testing.T, app *App, sessionID string, role database.Role, content string) {
	t.Helper()

	_, err := app.runtime.SessionRepository().AppendMessage(t.Context(), sessionID, nil, &database.MessageEntity{
		Timestamp: time.Time{},
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	})
	require.NoError(t, err)
}
