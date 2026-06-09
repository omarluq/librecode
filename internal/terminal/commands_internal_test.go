package terminal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

func TestRunSessionCommandDispatchesNotification(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	quit, err := app.runSessionCommand(context.Background(), "share", "", "/share")

	require.NoError(t, err)
	assert.False(t, quit)
	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, "/share"+commandNotImplemented, app.transcript.History[len(app.transcript.History)-1].Content)
}

func TestOpenCommandPanelKnownAndUnknownCommands(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	assert.False(t, app.openCommandPanel(context.Background(), "missing"))
	assert.True(t, app.openCommandPanel(context.Background(), commandLogin))
	assert.Equal(t, panelAuthLogin, app.selectedPanelKind)
}

func TestRenameSessionValidation(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	require.EqualError(t, app.renameSession(context.Background(), "name"), "no active session")
	app.sessionID = "session-1"
	require.EqualError(t, app.renameSession(context.Background(), ""), "name is required")
}

func TestSessionCommandNotificationsUseSharedPlaceholder(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	handler, ok := app.sessionCommandNotifications(context.Background(), "export")
	require.True(t, ok)

	handler()

	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, "/export"+commandNotImplemented, app.transcript.History[len(app.transcript.History)-1].Content)
}

func TestShowSessionInfoWithoutActiveSession(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.showSessionInfo(context.Background())

	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, database.RoleCustom, app.transcript.History[len(app.transcript.History)-1].Role)
	assert.Equal(t, "session: none", app.transcript.History[len(app.transcript.History)-1].Content)
}
