package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
	"github.com/omarluq/librecode/internal/transcript"
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

func TestApplySessionSelectionRoutesActivePromptCompletionToOriginalSession(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	original, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "original", "")
	require.NoError(t, err)
	target, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "target", "")
	require.NoError(t, err)

	app.sessionID = original.ID
	app.activePrompt = newTestActivePrompt(nil)
	app.activePrompt.SessionID = original.ID
	app.activePrompt.ID = 17
	app.working = true
	app.appendMessage(newChatMessage(transcript.RoleUser, "original prompt"))

	require.NoError(t, app.applySessionSelection(ctx, target.ID))
	app.handlePromptAsyncEvent(ctx, &asyncEvent{
		Response: &assistant.PromptResponse{
			SessionID: original.ID, UserEntryID: "", AssistantEntryID: "", Text: "original response",
			Thinking: nil, ToolEvents: nil, Usage: model.EmptyTokenUsage(), Cached: false,
		},
		ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventPromptDone, Provider: "", Text: "", PromptID: 17,
	})

	assert.Equal(t, target.ID, app.sessionID)
	assert.Nil(t, app.activePrompt)
	assert.NotContains(t, transcriptContents(app.transcript.History), "original response")
	require.True(t, app.restoreSessionView(original.ID))
	assert.Contains(t, transcriptContents(app.transcript.History), "original response")
}

func TestApplySessionSelectionPreservesDisplayedSessionState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "current", "")
	require.NoError(t, err)

	app.sessionID = session.ID
	app.composerBuffer.SetText("draft")
	app.queuedMessages = promptDrafts("queued draft")
	app.steeringMessages = promptDrafts("steering")
	app.appendStreamingBlock(transcript.RoleAssistant, "streaming")
	app.scrollOffset = 4
	app.agentTaskSessionStack = []string{selectionParentID}
	app.agentTasks = make([]database.AgentTaskEntity, 1)
	app.deliveredAgentTasks = map[string]struct{}{selectionTaskID: {}}
	watchCanceled := false
	app.agentTaskWatches[selectionTaskID] = func() { watchCanceled = true }

	require.NoError(t, app.applySessionSelection(ctx, session.ID))

	assert.Equal(t, "draft", app.composerBuffer.TextValue())
	assert.Equal(t, []string{"queued draft"}, promptDraftTexts(app.queuedMessages))
	assert.Equal(t, []string{"steering"}, promptDraftTexts(app.steeringMessages))
	require.Len(t, app.transcript.Streaming.Blocks, 1)
	assert.Equal(t, "streaming", app.transcript.Streaming.Blocks[0].Content)
	assert.Equal(t, 4, app.scrollOffset)
	assert.Equal(t, []string{selectionParentID}, app.agentTaskSessionStack)
	assert.Len(t, app.agentTasks, 1)
	assert.Contains(t, app.deliveredAgentTasks, selectionTaskID)
	assert.Contains(t, app.agentTaskWatches, selectionTaskID)
	assert.False(t, watchCanceled)
}

func TestApplySessionSelectionIsolatesTransientSessionState(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	original, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "original", "")
	require.NoError(t, err)
	target, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "target", "")
	require.NoError(t, err)

	app.sessionID = original.ID
	app.queuedMessages = promptDrafts("queued follow-up")
	app.hiddenQueuedMessages = promptDrafts("hidden follow-up")
	app.steeringMessages = promptDrafts("pending steering")
	app.liveAgentCompletions = []chatMessage{newChatMessage(transcript.RoleAssistant, "completion")}
	app.composerBuffer.SetText("draft")
	app.appendStreamingBlock(transcript.RoleAssistant, "streaming")

	require.NoError(t, app.applySessionSelection(ctx, target.ID))

	assert.Empty(t, app.queuedMessages)
	assert.Empty(t, app.hiddenQueuedMessages)
	assert.Empty(t, app.steeringMessages)
	assert.Empty(t, app.liveAgentCompletions)
	assert.True(t, app.composerDraftEmpty())
	assert.Empty(t, app.transcript.Streaming.Blocks)

	require.True(t, app.restoreSessionView(original.ID))
	assert.Equal(t, []string{"queued follow-up"}, promptDraftTexts(app.queuedMessages))
	assert.Equal(t, []string{"hidden follow-up"}, promptDraftTexts(app.hiddenQueuedMessages))
	assert.Equal(t, []string{"pending steering"}, promptDraftTexts(app.steeringMessages))
	require.Len(t, app.liveAgentCompletions, 1)
	assert.Equal(t, "completion", app.liveAgentCompletions[0].Content)
	assert.Equal(t, "draft", app.composerBuffer.TextValue())
	require.Len(t, app.transcript.Streaming.Blocks, 1)
	assert.Equal(t, "streaming", app.transcript.Streaming.Blocks[0].Content)
}

func TestApplySessionSelectionRefreshesRestoredViewWithPersistedMessages(t *testing.T) {
	t.Parallel()

	ctx := t.Context()
	app := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
	original, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "original", "")
	require.NoError(t, err)
	target, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "target", "")
	require.NoError(t, err)

	app.sessionID = original.ID
	app.composerBuffer.SetText("original draft")

	require.NoError(t, app.applySessionSelection(ctx, target.ID))
	_, err = app.runtime.SessionRepository().AppendMessage(ctx, original.ID, nil, &database.MessageEntity{
		Timestamp: time.Now().UTC(), Role: database.RoleAssistant, Content: "persisted while hidden",
		Provider: "", Model: "", Parts: nil,
	})
	require.NoError(t, err)

	require.NoError(t, app.applySessionSelection(ctx, original.ID))

	assert.Equal(t, "original draft", app.composerBuffer.TextValue())
	contents := transcriptContents(app.transcript.History)
	count := 0

	for _, content := range contents {
		if content == "persisted while hidden" {
			count++
		}
	}

	assert.Equal(t, 1, count)
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
		Model:     "", Parts: nil,
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
