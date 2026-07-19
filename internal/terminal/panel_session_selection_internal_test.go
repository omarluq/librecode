package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
)

const (
	selectionTaskID   = "selection-task"
	selectionParentID = "parent-session"
)

func TestApplySessionSelectionPreservesStateWhenLoadFails(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	target, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "target", "")
	require.NoError(t, err)

	app.openSessionPanel(ctx)
	openPanel := app.panel
	app.sessionID = "current-session"
	app.addSystemMessage("existing transcript")
	app.agentTaskSessionStack = []string{selectionParentID}
	app.agentTasks = make([]database.AgentTaskEntity, 1)
	app.deliveredAgentTasks = map[string]struct{}{selectionTaskID: {}}
	watchCanceled := false
	app.agentTaskWatches[selectionTaskID] = func() { watchCanceled = true }
	app.settings = nil

	canceledCtx, cancel := context.WithCancel(ctx)
	cancel()
	require.Error(t, app.applySessionSelection(canceledCtx, target.ID))

	assert.Equal(t, "current-session", app.sessionID)
	require.Len(t, app.transcript.History, 1)
	assert.Equal(t, "existing transcript", app.transcript.History[0].Content)
	assert.Equal(t, []string{selectionParentID}, app.agentTaskSessionStack)
	assert.Len(t, app.agentTasks, 1)
	assert.Contains(t, app.deliveredAgentTasks, selectionTaskID)
	assert.Contains(t, app.agentTaskWatches, selectionTaskID)
	assert.False(t, watchCanceled)
	assert.Same(t, openPanel, app.panel)
	assert.Equal(t, panelSessions, app.selectedPanelKind)
}

func TestApplySessionSelectionAddsMessageAfterSuccessfulLoad(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))

	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "test", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	_, err = app.runtime.SessionRepository().AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Time{},
		Role:      database.RoleAssistant,
		Content:   interruptTestPrompt,
		Provider:  "",
		Model:     "",
	})
	if err != nil {
		t.Fatalf("append message: %v", err)
	}

	if err := app.applySessionSelection(ctx, session.ID); err != nil {
		t.Fatalf("applySessionSelection error = %v", err)
	}

	if got, want := len(app.transcript.History), 2; got != want {
		t.Fatalf("len(messages) = %d, want %d", got, want)
	}

	if got, want := app.transcript.History[0].Content, interruptTestPrompt; got != want {
		t.Fatalf("messages[0].Content = %q, want %q", got, want)
	}

	if got, want := app.transcript.History[1].Content, "resumed session: "+session.ID; got != want {
		t.Fatalf("messages[1].Content = %q, want %q", got, want)
	}
}
