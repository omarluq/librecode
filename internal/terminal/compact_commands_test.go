//nolint:testpackage // These tests exercise unexported compact command helpers.
package terminal

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/omarluq/librecode/internal/assistant"
	"github.com/omarluq/librecode/internal/database"
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
	case <-time.After(time.Second):
		t.Fatal("compaction should start provider request asynchronously")
	}
	if !app.compacting {
		t.Fatal("app should still be compacting while provider is blocked")
	}
	client.finish("summary of compacted work", nil)
	event := readPromptAsyncEvent(t, app)
	if got := event.Kind; got != asyncEventCompactDone {
		t.Fatalf("event.Kind = %q, want %q", got, asyncEventCompactDone)
	}
}

func TestHandleCompactDoneUpdatesState(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.compacting = true
	app.activeCompaction = &activeCompactionState{Cancel: func() {}, ID: 9}

	handled := app.handleCompactAsyncEvent(&asyncEvent{
		Response:  nil,
		ToolEvent: nil,
		Usage:     nil,
		Kind:      asyncEventCompactDone,
		Provider:  "kept-entry",
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
	if app.pendingParentID == nil || *app.pendingParentID != "kept-entry" {
		t.Fatalf("pendingParentID = %v, want kept-entry", app.pendingParentID)
	}
	if got := app.transcript.History[len(app.transcript.History)-1].Content; got != compactedStatusMessage {
		t.Fatalf("last message = %q, want compacted message", got)
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
