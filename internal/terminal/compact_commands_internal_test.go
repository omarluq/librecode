package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/model"
)

func TestCompactSessionStartsAsyncWork(t *testing.T) {
	t.Parallel()

	client := newBlockingTerminalClient()
	app := newPromptSendTestApp(t, client)
	app.screen = newClipboardScreen()

	session, err := app.runtime.SessionRepository().CreateSession(context.Background(), app.cwd, "compact", "")
	require.NoError(t, err)

	first := appendTerminalCompactMessage(t, app, session.ID, nil, database.RoleUser, "old user goal")
	second := appendTerminalCompactMessage(
		t,
		app,
		session.ID,
		&first.ID,
		database.RoleAssistant,
		strings.Repeat("old assistant answer ", 5_000),
	)
	third := appendTerminalCompactMessage(t, app, session.ID, &second.ID, database.RoleUser, "recent user tail")
	_ = appendTerminalCompactMessage(t, app, session.ID, &third.ID, database.RoleAssistant, "recent assistant tail")
	app.sessionID = session.ID

	require.NoError(t, app.compactSession(context.Background()))
	assert.True(t, app.compacting)
	assert.NotNil(t, app.activeCompaction)
	assert.Equal(t, "Compacting context...", app.workingLoaderText())
	require.Eventually(t, client.readyClosed, 3*time.Second, 10*time.Millisecond)
	assert.True(t, app.compacting)

	client.finish("summary of compacted work", nil)
	assertCompactDoneEventHasUsage(t, readPromptAsyncEvent(t, app))
}

func TestPostCompactDoneAllowsNilUsage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}
	app.tokenUsage = model.TokenUsage{
		Breakdown:       nil,
		TopContributors: nil,
		ContextWindow:   100_000,
		ContextTokens:   42_000,
		InputTokens:     42_000,
		OutputTokens:    0,
	}

	app.applyCompactDone(context.Background(), &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventCompactDone,
		Provider:      compactTestEntryID,
		Text:          compactedStatusMessage,
		PromptID:      9,
	})

	assert.False(t, app.compacting)
	assert.Equal(t, 42_000, app.tokenUsage.ContextTokens)
	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, compactedStatusMessage, app.transcript.History[len(app.transcript.History)-1].Content)
}

func assertCompactDoneEventHasUsage(t *testing.T, event *asyncEvent) {
	t.Helper()

	assert.Equal(t, asyncEventCompactDone, event.Kind)
	require.NotNil(t, event.Usage)
	assert.Positive(t, event.Usage.ContextTokens)
}

const (
	compactTestSessionID = "compact-session"
	compactTestIgnored   = "compact-ignored"
	compactTestParentID  = "compact-parent"
	compactTestEntryID   = "compaction-entry"
	compactTestFailed    = "compaction failed"
)

func TestCompactSessionValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup   func(*testing.T, *App)
		name    string
		wantErr string
	}{
		{
			name:    "runtime missing",
			setup:   func(*testing.T, *App) {},
			wantErr: "runtime is not configured",
		},
		{
			name: "session missing",
			setup: func(t *testing.T, app *App) {
				t.Helper()
				configured := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
				app.runtime = configured.runtime
			},
			wantErr: "no active session",
		},
		{
			name: "busy",
			setup: func(t *testing.T, app *App) {
				t.Helper()
				configured := newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))
				app.runtime = configured.runtime
				app.sessionID = compactTestSessionID
				app.working = true
			},
			wantErr: "another operation is already running",
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			testCase.setup(t, app)

			err := app.compactSession(context.Background())

			require.Error(t, err)
			assert.ErrorContains(t, err, testCase.wantErr)
		})
	}
}

func TestHandleCompactDoneUpdatesState(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}

	handled := app.handleCompactAsyncEvent(context.Background(), &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventCompactDone,
		Provider:      compactTestEntryID,
		Text:          compactedStatusMessage,
		PromptID:      9,
	})

	assert.True(t, handled)
	assert.False(t, app.compacting)
	assert.Nil(t, app.activeCompaction)
	require.NotNil(t, app.pendingParentID)
	assert.Equal(t, compactTestEntryID, *app.pendingParentID)
	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, compactedStatusMessage, app.transcript.History[len(app.transcript.History)-1].Content)
}

func TestHandleCompactDoneStartsQueuedPrompt(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("queued response"), nil)
	app := newPromptSendTestApp(t, client)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}
	app.queuedMessages = []string{"queued after compact"}

	app.applyCompactDone(context.Background(), &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: &model.TokenUsage{
			Breakdown:       nil,
			TopContributors: nil,
			ContextWindow:   100_000,
			ContextTokens:   10_000,
			InputTokens:     10_000,
			OutputTokens:    0,
		},
		Kind: asyncEventCompactDone, Provider: "", Text: compactedStatusMessage, PromptID: 9,
	})

	request := waitForPromptRequest(t, client)
	assert.Equal(t, "queued after compact", request.Messages[len(request.Messages)-1].Content)
	assert.Empty(t, app.queuedMessages)
	assert.Equal(t, 10_000, app.tokenUsage.ContextTokens)
}

type compactTransitionCase struct {
	setupApp        func(t *testing.T, app *App) func(t *testing.T)
	invoke          func(t *testing.T, app *App)
	wantLastMessage string
	wantStatus      string
	name            string
	wantEventKind   asyncEventKind
}

func TestCompactCommandStateTransitions(t *testing.T) {
	t.Parallel()

	for _, testCase := range compactTransitionCases() {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			afterInvoke := testCase.setupApp(t, app)
			testCase.invoke(t, app)

			if afterInvoke != nil {
				afterInvoke(t)
			}

			assertCompactTransition(t, app, &testCase)
		})
	}
}

func compactTransitionCases() []compactTransitionCase {
	return []compactTransitionCase{
		{
			setupApp:        setupCompactErrorTransition,
			invoke:          invokeCompactErrorTransition,
			wantLastMessage: compactTestFailed,
			wantStatus:      "",
			name:            "compact error updates state",
			wantEventKind:   "",
		},
		{
			setupApp:        setupRunCompactErrorTransition,
			invoke:          invokeRunCompactErrorTransition,
			wantLastMessage: "provider down",
			wantStatus:      "",
			name:            "run compact posts provider error",
			wantEventKind:   asyncEventCompactError,
		},
		{
			setupApp:        setupCancelCompactTransition,
			invoke:          invokeCancelCompactTransition,
			wantLastMessage: "",
			wantStatus:      "context compaction canceled",
			name:            "cancel active operation cancels compaction",
			wantEventKind:   "",
		},
	}
}

func setupCompactErrorTransition(t *testing.T, app *App) func(t *testing.T) {
	t.Helper()

	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}

	return nil
}

func invokeCompactErrorTransition(t *testing.T, app *App) {
	t.Helper()

	handled := app.handleCompactAsyncEvent(context.Background(), &asyncEvent{
		Response:      nil,
		ToolCallEvent: nil,
		ToolEvent:     nil,
		Usage:         nil,
		Kind:          asyncEventCompactError,
		Provider:      "",
		Text:          compactTestFailed,
		PromptID:      9,
	})
	assert.True(t, handled)
}

func setupRunCompactErrorTransition(t *testing.T, app *App) func(t *testing.T) {
	t.Helper()

	client := newBlockingTerminalClient()
	configuredApp := newPromptSendTestApp(t, client)
	*app = *configuredApp
	app.screen = newClipboardScreen()
	session := createCompactTestSession(t, app)
	app.sessionID = session.ID

	return func(t *testing.T) {
		t.Helper()

		require.Eventually(t, client.readyClosed, 3*time.Second, 10*time.Millisecond)
		client.finish("", errors.New("provider down"))
	}
}

func createCompactTestSession(t *testing.T, app *App) *database.SessionEntity {
	t.Helper()

	session, err := app.runtime.SessionRepository().CreateSession(context.Background(), app.cwd, "compact", "")
	require.NoError(t, err)

	first := appendTerminalCompactMessage(t, app, session.ID, nil, database.RoleUser, "old user goal")
	_ = appendTerminalCompactMessage(
		t,
		app,
		session.ID,
		&first.ID,
		database.RoleAssistant,
		strings.Repeat("old assistant answer ", 5_000),
	)

	return session
}

func invokeRunCompactErrorTransition(t *testing.T, app *App) {
	t.Helper()

	compactCtx, cancel := context.WithCancel(context.Background())
	go app.runCompactSession(context.Background(), compactCtx, cancel, 42, nil)
}

func setupCancelCompactTransition(t *testing.T, app *App) func(t *testing.T) {
	t.Helper()

	canceled := false
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() { canceled = true }, ID: 7, QueuedStart: 0}

	return func(t *testing.T) {
		t.Helper()

		assert.True(t, canceled)
	}
}

func invokeCancelCompactTransition(t *testing.T, app *App) {
	t.Helper()

	app.cancelActiveOperation(context.Background())
}

func assertCompactTransition(t *testing.T, app *App, testCase *compactTransitionCase) {
	t.Helper()

	if testCase.wantEventKind != "" {
		assertCompactAsyncEvent(t, app, testCase)

		return
	}

	assert.False(t, app.compacting)
	assert.Nil(t, app.activeCompaction)

	if testCase.wantStatus != "" {
		assert.Equal(t, testCase.wantStatus, app.statusMessage)
	}

	if testCase.wantLastMessage != "" {
		require.NotEmpty(t, app.transcript.History)
		assert.Equal(t, testCase.wantLastMessage, app.transcript.History[len(app.transcript.History)-1].Content)
	}
}

func assertCompactAsyncEvent(t *testing.T, app *App, testCase *compactTransitionCase) {
	t.Helper()

	event := readPromptAsyncEvent(t, app)
	assert.Equal(t, testCase.wantEventKind, event.Kind)
	assert.Contains(t, event.Text, testCase.wantLastMessage)
}

func appendTerminalCompactMessage(
	t *testing.T,
	app *App,
	sessionID string,
	parentID *string,
	role database.Role,
	content string,
) *database.EntryEntity {
	t.Helper()

	message := &database.MessageEntity{
		Timestamp: time.Now().UTC(),
		Role:      role,
		Content:   content,
		Provider:  "",
		Model:     "",
	}

	entry, err := app.runtime.SessionRepository().AppendMessage(context.Background(), sessionID, parentID, message)
	require.NoError(t, err)

	return entry
}

type blockingTerminalClient struct {
	ready   chan struct{}
	finishC chan blockingTerminalResult
}

type blockingTerminalResult struct {
	err     error
	summary string
}

func newBlockingTerminalClient() *blockingTerminalClient {
	return &blockingTerminalClient{
		ready:   make(chan struct{}),
		finishC: make(chan blockingTerminalResult, 1),
	}
}

func (client *blockingTerminalClient) Complete(
	_ context.Context,
	_ *assistant.CompletionRequest,
) (*assistant.CompletionResult, error) {
	select {
	case <-client.ready:
	default:
		close(client.ready)
	}

	result := <-client.finishC
	if result.err != nil {
		return nil, result.err
	}

	return newTerminalCompletionResult(result.summary), nil
}

func (client *blockingTerminalClient) finish(summary string, err error) {
	client.finishC <- blockingTerminalResult{err: err, summary: summary}
}

func (client *blockingTerminalClient) readyClosed() bool {
	select {
	case <-client.ready:
		return true
	default:
		return false
	}
}

func TestHandleCompactAsyncEventIgnoresStaleAndNonCompactEvents(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}

	handled := app.handleCompactAsyncEvent(context.Background(), &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventCompactDone, Provider: "stale", Text: compactTestIgnored, PromptID: 10,
	})
	assert.True(t, handled)
	assert.True(t, app.compacting)
	assert.Nil(t, app.pendingParentID)

	handled = app.handleCompactAsyncEvent(context.Background(), &asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventPromptUsageSnapshot, Provider: "", Text: "not compact", PromptID: 9,
	})
	assert.False(t, handled)
}

func TestHandleCompactAsyncEventPassesThroughAutoCompactionEvents(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		kind asyncEventKind
	}{
		{name: "start", kind: asyncEventCompactStart},
		{name: statusDone, kind: asyncEventCompactDone},
		{name: "error", kind: asyncEventCompactError},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)

			handled := app.handleCompactAsyncEvent(context.Background(), &asyncEvent{
				Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
				Kind: testCase.kind, Provider: "", Text: "auto compact", PromptID: 9,
			})

			assert.False(t, handled,
				"auto-compaction event should pass through prompt lifecycle when no manual compaction is active")
			assert.False(t, app.compacting)
		})
	}
}

func TestApplyCompactErrorDefaultMessage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}

	app.applyCompactError(&asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventCompactError, Provider: "", Text: "", PromptID: 9,
	})

	assert.False(t, app.compacting)
	assert.Nil(t, app.activeCompaction)
	require.NotEmpty(t, app.transcript.History)
	assert.Equal(t, "context compaction failed", app.transcript.History[len(app.transcript.History)-1].Content)
}

func TestCompactErrorRestoresQueuedPrompt(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.queuedMessages = []string{"preexisting", "during compaction"}
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 1}

	app.applyCompactError(&asyncEvent{
		Response: nil, ToolCallEvent: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventCompactError, Provider: "", Text: compactTestFailed, PromptID: 9,
	})

	assert.Equal(t, "during compaction", app.composerBuffer.TextValue())
	assert.Equal(t, []string{"preexisting"}, app.queuedMessages)
}

func TestCompactFormattingHelpers(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	parentID := compactTestParentID
	app.pendingParentID = &parentID

	cloned := app.compactionParentEntryID()
	require.NotNil(t, cloned)
	assert.Equal(t, parentID, *cloned)

	*cloned = "changed"

	require.NotNil(t, app.pendingParentID)
	assert.Equal(t, parentID, *app.pendingParentID)
	assert.Equal(t, compactedStatusMessage, compactDoneText(nil))
	assert.Empty(t, compactionEntryID(nil))
	assert.Nil(t, nonEmptyStringPtr("   "))
}
