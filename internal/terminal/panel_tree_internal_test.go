package terminal

import (
	"context"
	"testing"
	"time"

	"github.com/omarluq/librecode/internal/database"
)

func TestTreeDescription(t *testing.T) {
	t.Parallel()

	entry := testEntryEntity()

	entry.Message.Content = "hello"
	if got, want := treeDescription(&entry), "hello"; got != want {
		t.Fatalf("treeDescription(content) = %q, want %q", got, want)
	}

	entry = testEntryEntity()

	entry.Summary = "summary"
	if got, want := treeDescription(&entry), "summary"; got != want {
		t.Fatalf("treeDescription(summary) = %q, want %q", got, want)
	}

	entry = testEntryEntity()

	const testGPT5Model = "gpt-5"

	entry.Message.Provider = testProviderOpenAI

	entry.Message.Model = testGPT5Model
	if got, want := treeDescription(&entry), testProviderOpenAI+"/"+testGPT5Model; got != want {
		t.Fatalf("treeDescription(model) = %q, want %q", got, want)
	}
}

func TestTreePanelFlow(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	app, userEntryID := newTreePanelTestApp(ctx, t)

	app.openTreePanel(ctx)

	if got, want := app.selectedPanelKind, panelTree; got != want {
		t.Fatalf("selectedPanelKind = %q, want %q", got, want)
	}

	if app.panel == nil || app.panel.Kind() != panelTree {
		t.Fatal("tree panel should be open")
	}

	if len(app.panel.Items()) == 0 {
		t.Fatal("tree panel should include session entries")
	}

	if err := app.applyTreeSelection(ctx, userEntryID); err != nil {
		t.Fatalf("applyTreeSelection error = %v", err)
	}

	if app.pendingParentID == nil || *app.pendingParentID != "" {
		t.Fatalf("pendingParentID = %v, want root branch pointer", app.pendingParentID)
	}

	if got, want := app.composerBuffer.TextValue(), interruptTestPrompt; got != want {
		t.Fatalf("composer text = %q, want %q", got, want)
	}
}

func newTreePanelTestApp(ctx context.Context, t *testing.T) (app *App, userEntryID string) {
	t.Helper()

	app = newPromptSendTestApp(t, newTerminalPromptClient(newTerminalCompletionResult("ok"), nil))

	session, err := app.runtime.SessionRepository().CreateSession(ctx, app.cwd, "tree", "")
	if err != nil {
		t.Fatalf("create session: %v", err)
	}

	userEntry, err := app.runtime.SessionRepository().AppendMessage(ctx, session.ID, nil, &database.MessageEntity{
		Timestamp: time.Time{},
		Role:      database.RoleUser,
		Content:   interruptTestPrompt,
		Provider:  "",
		Model:     "",
	})
	if err != nil {
		t.Fatalf("append user entry: %v", err)
	}

	_, err = app.runtime.SessionRepository().AppendMessage(ctx, session.ID, &userEntry.ID, &database.MessageEntity{
		Timestamp: time.Time{},
		Role:      database.RoleAssistant,
		Content:   "world",
		Provider:  "",
		Model:     "",
	})
	if err != nil {
		t.Fatalf("append assistant entry: %v", err)
	}

	app.sessionID = session.ID

	userEntryID = userEntry.ID

	return app, userEntryID
}

func TestEmptyParentID(t *testing.T) {
	t.Parallel()

	if got := emptyParentID(nil); got == nil || *got != "" {
		t.Fatal("emptyParentID(nil) should return pointer to empty string")
	}

	parent := "parent"
	if got := emptyParentID(&parent); got == nil || *got != "parent" {
		t.Fatal("emptyParentID should return original value when non-nil")
	}
}
