//nolint:testpackage // These tests exercise unexported compact command helpers.
package terminal

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

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
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
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

	err = app.compactSession(context.Background())

	if err != nil {
		t.Fatalf("compactSession returned error: %v", err)
	}
	if !app.compacting {
		t.Fatal("app should be marked compacting")
	}
	if app.activeCompaction == nil {
		t.Fatal("activeCompaction should be set")
	}
	if got := app.workingLoaderText(); got != "Compacting context..." {
		t.Fatalf("workingLoaderText = %q, want compacting label", got)
	}
	select {
	case <-client.ready:
	case <-time.After(3 * time.Second):
		t.Fatal("compaction should start provider request asynchronously")
	}
	if !app.compacting {
		t.Fatal("app should still be compacting while provider is blocked")
	}
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
		Response:  nil,
		ToolEvent: nil,
		Usage:     nil,
		Kind:      asyncEventCompactDone,
		Provider:  compactTestEntryID,
		Text:      compactedStatusMessage,
		PromptID:  9,
	})

	if app.compacting {
		t.Fatal("app should stop compacting")
	}
	if got := app.tokenUsage.ContextTokens; got != 42_000 {
		t.Fatalf("tokenUsage.ContextTokens = %d, want previous usage preserved", got)
	}
	if got := app.transcript.History[len(app.transcript.History)-1].Content; got != compactedStatusMessage {
		t.Fatalf("last message = %q, want compacted message", got)
	}
}

func assertCompactDoneEventHasUsage(t *testing.T, event *asyncEvent) {
	t.Helper()

	if got := event.Kind; got != asyncEventCompactDone {
		t.Fatalf("event.Kind = %q, want %q", got, asyncEventCompactDone)
	}
	if event.Usage == nil || event.Usage.ContextTokens <= 0 {
		t.Fatalf("compact done usage = %v, want refreshed usage", event.Usage)
	}
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

			if err == nil || !strings.Contains(err.Error(), testCase.wantErr) {
				t.Fatalf("compactSession error = %v, want %q", err, testCase.wantErr)
			}
		})
	}
}

func TestHandleCompactDoneUpdatesState(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}

	handled := app.handleCompactAsyncEvent(context.Background(), &asyncEvent{
		Response:  nil,
		ToolEvent: nil,
		Usage:     nil,
		Kind:      asyncEventCompactDone,
		Provider:  compactTestEntryID,
		Text:      compactedStatusMessage,
		PromptID:  9,
	})

	if !handled {
		t.Fatal("compact event should be handled")
	}
	if app.compacting {
		t.Fatal("app should stop compacting")
	}
	if app.activeCompaction != nil {
		t.Fatal("activeCompaction should be cleared")
	}
	if app.pendingParentID == nil || *app.pendingParentID != compactTestEntryID {
		t.Fatalf("pendingParentID = %v, want %s", app.pendingParentID, compactTestEntryID)
	}
	if got := app.transcript.History[len(app.transcript.History)-1].Content; got != compactedStatusMessage {
		t.Fatalf("last message = %q, want compacted message", got)
	}
}

func TestHandleCompactDoneStartsQueuedPrompt(t *testing.T) {
	t.Parallel()

	client := newTerminalPromptClient(newTerminalCompletionResult("queued response"), nil)
	app := newPromptSendTestApp(t, client)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}
	app.queuedMessages = []string{"queued after compact"}

	app.applyCompactDone(context.Background(), &asyncEvent{
		Response: nil, ToolEvent: nil, Usage: &model.TokenUsage{
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
	if got := request.Messages[len(request.Messages)-1].Content; got != "queued after compact" {
		t.Fatalf("queued request text = %q", got)
	}
	if len(app.queuedMessages) != 0 {
		t.Fatalf("queuedMessages length = %d, want 0", len(app.queuedMessages))
	}
	if got := app.tokenUsage.ContextTokens; got != 10_000 {
		t.Fatalf("tokenUsage.ContextTokens = %d, want 10000", got)
	}
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
		Response:  nil,
		ToolEvent: nil,
		Usage:     nil,
		Kind:      asyncEventCompactError,
		Provider:  "",
		Text:      compactTestFailed,
		PromptID:  9,
	})
	if !handled {
		t.Fatal("compact error event should be handled")
	}
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

		select {
		case <-client.ready:
		case <-time.After(3 * time.Second):
			t.Fatal("compaction should start provider request")
		}
		client.finish("", errors.New("provider down"))
	}
}

func createCompactTestSession(t *testing.T, app *App) *database.SessionEntity {
	t.Helper()

	session, err := app.runtime.SessionRepository().CreateSession(context.Background(), app.cwd, "compact", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}
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

		if !canceled {
			t.Fatal("active compaction cancel should be called")
		}
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
	if app.compacting {
		t.Fatal("app should stop compacting")
	}
	if app.activeCompaction != nil {
		t.Fatal("activeCompaction should be cleared")
	}
	if testCase.wantStatus != "" && app.statusMessage != testCase.wantStatus {
		t.Fatalf("statusMessage = %q, want %q", app.statusMessage, testCase.wantStatus)
	}
	if testCase.wantLastMessage != "" {
		got := app.transcript.History[len(app.transcript.History)-1].Content
		if got != testCase.wantLastMessage {
			t.Fatalf("last message = %q, want %q", got, testCase.wantLastMessage)
		}
	}
}

func assertCompactAsyncEvent(t *testing.T, app *App, testCase *compactTransitionCase) {
	t.Helper()

	event := readPromptAsyncEvent(t, app)
	if got := event.Kind; got != testCase.wantEventKind {
		t.Fatalf("event.Kind = %q, want %q", got, testCase.wantEventKind)
	}
	if !strings.Contains(event.Text, testCase.wantLastMessage) {
		t.Fatalf("event.Text = %q, want %q", event.Text, testCase.wantLastMessage)
	}
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
	if err != nil {
		t.Fatalf("append message: %v", err)
	}

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

func TestHandleCompactAsyncEventIgnoresStaleAndNonCompactEvents(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}

	handled := app.handleCompactAsyncEvent(context.Background(), &asyncEvent{
		Response: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventCompactDone, Provider: "stale", Text: compactTestIgnored, PromptID: 10,
	})
	if !handled {
		t.Fatal("compact done event should be handled even when stale")
	}
	if !app.compacting {
		t.Fatal("stale compaction event should not stop active compaction")
	}
	if app.pendingParentID != nil {
		t.Fatalf("pendingParentID = %v, want nil", app.pendingParentID)
	}

	handled = app.handleCompactAsyncEvent(context.Background(), &asyncEvent{
		Response: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventPromptUsageSnapshot, Provider: "", Text: "not compact", PromptID: 9,
	})
	if handled {
		t.Fatal("non-compact event should not be handled")
	}
}

func TestApplyCompactErrorDefaultMessage(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 0}

	app.applyCompactError(&asyncEvent{
		Response: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventCompactError, Provider: "", Text: "", PromptID: 9,
	})

	if app.compacting {
		t.Fatal("app should stop compacting")
	}
	if app.activeCompaction != nil {
		t.Fatal("activeCompaction should be cleared")
	}
	if got := app.transcript.History[len(app.transcript.History)-1].Content; got != "context compaction failed" {
		t.Fatalf("last message = %q, want default failure", got)
	}
}

func TestCompactErrorRestoresQueuedPrompt(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.queuedMessages = []string{"preexisting", "during compaction"}
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9, QueuedStart: 1}

	app.applyCompactError(&asyncEvent{
		Response: nil, ToolEvent: nil, Usage: nil,
		Kind: asyncEventCompactError, Provider: "", Text: compactTestFailed, PromptID: 9,
	})

	if got, want := app.composerText(), "during compaction"; got != want {
		t.Fatalf("composer text = %q, want %q", got, want)
	}
	if got, want := app.queuedMessages, []string{"preexisting"}; len(got) != len(want) || got[0] != want[0] {
		t.Fatalf("queuedMessages = %v, want %v", got, want)
	}
}

func TestCompactFormattingHelpers(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	parentID := compactTestParentID
	app.pendingParentID = &parentID
	cloned := app.compactionParentEntryID()
	if cloned == nil || *cloned != parentID {
		t.Fatalf("compactionParentEntryID = %v, want parent", cloned)
	}
	*cloned = "changed"
	if *app.pendingParentID != parentID {
		t.Fatal("compactionParentEntryID should clone pending parent")
	}

	if got := compactDoneText(nil); got != compactedStatusMessage {
		t.Fatalf("compactDoneText(nil) = %q", got)
	}
	if got := compactionEntryID(nil); got != "" {
		t.Fatalf("compactionEntryID(nil) = %q, want empty", got)
	}
	if got := nonEmptyStringPtr("   "); got != nil {
		t.Fatalf("nonEmptyStringPtr(blank) = %v, want nil", got)
	}
}
