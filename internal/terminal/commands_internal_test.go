package terminal

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/transcript"
)

func TestRunSessionCommandDispatchesNotification(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	quit, err := app.runSessionCommand(context.Background(), "auth", "", "/auth")

	require.NoError(t, err)
	assert.False(t, quit)
	require.NotEmpty(t, app.transcript.History)
	assert.Contains(t, app.transcript.History[len(app.transcript.History)-1].Content, "auth status:")
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
	app.sessionID = workflowTestSessionID
	require.EqualError(t, app.renameSession(context.Background(), ""), "name is required")
}

func TestRemovedPlaceholderSlashCommandsFallThroughToPrompt(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	for _, command := range []string{"export", "import", "share"} {
		_, ok := app.sessionCommandNotifications(context.Background(), command)
		assert.False(t, ok)
	}
}

func TestShowSessionInfoWithoutActiveSession(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	app.showSessionInfo(context.Background())

	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, transcript.RoleCustom, app.transcript.History[len(app.transcript.History)-1].Role)
	assert.Equal(t, "session: none", app.transcript.History[len(app.transcript.History)-1].Content)
}
