//nolint:testpackage // These tests exercise unexported terminal input helpers.
package terminal

import (
	"context"
	"testing"

	"github.com/gdamore/tcell/v3"
)

func TestPromptHistoryNavigatesPromptsAndRestoresDraft(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.recordPromptHistory("first prompt")
	app.recordPromptHistory("second prompt")
	app.setComposerText("draft prompt")

	pressTerminalKey(t, app, tcell.KeyUp, "")
	assertEditorText(t, app, "second prompt")
	pressTerminalKey(t, app, tcell.KeyUp, "")
	assertEditorText(t, app, "first prompt")
	pressTerminalKey(t, app, tcell.KeyUp, "")
	assertEditorText(t, app, "first prompt")
	pressTerminalKey(t, app, tcell.KeyDown, "")
	assertEditorText(t, app, "second prompt")
	pressTerminalKey(t, app, tcell.KeyDown, "")
	assertEditorText(t, app, "draft prompt")
}

func TestPromptHistoryEditBecomesDraft(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.recordPromptHistory("first prompt")
	app.recordPromptHistory("second prompt")

	pressTerminalKey(t, app, tcell.KeyUp, "")
	assertEditorText(t, app, "second prompt")
	pressTerminalKey(t, app, tcell.KeyRune, "!")
	assertEditorText(t, app, "second prompt!")
	pressTerminalKey(t, app, tcell.KeyUp, "")
	assertEditorText(t, app, "second prompt")
	pressTerminalKey(t, app, tcell.KeyDown, "")
	assertEditorText(t, app, "second prompt!")
}

func TestPromptHistoryRecordsSubmittedCommands(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.setComposerText(" /quit ")

	shouldQuit, err := app.submit(context.Background())
	if err != nil {
		t.Fatalf("submit returned error: %v", err)
	}
	if !shouldQuit {
		t.Fatal("submit should quit for /quit")
	}

	pressTerminalKey(t, app, tcell.KeyUp, "")
	assertEditorText(t, app, "/quit")
}

func TestCtrlCClearsComposerText(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.recordPromptHistory("previous prompt")
	app.setComposerText("draft text")

	pressTerminalKey(t, app, tcell.KeyCtrlC, "")

	assertEditorText(t, app, "")
	pressTerminalKey(t, app, tcell.KeyUp, "")
	assertEditorText(t, app, "previous prompt")
}

func TestCtrlCExitsWhenComposerEmpty(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)

	shouldQuit, err := app.handleKey(context.Background(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModNone))
	if err != nil {
		t.Fatalf("handleKey returned error: %v", err)
	}
	if shouldQuit {
		t.Fatal("first Ctrl+C should not quit")
	}
	shouldQuit, err = app.handleKey(context.Background(), tcell.NewEventKey(tcell.KeyCtrlC, "", tcell.ModNone))
	if err != nil {
		t.Fatalf("handleKey returned error: %v", err)
	}
	if !shouldQuit {
		t.Fatal("second Ctrl+C should quit")
	}
}

func pressTerminalKey(t *testing.T, app *App, key tcell.Key, text string) {
	t.Helper()

	shouldQuit, err := app.handleKey(context.Background(), tcell.NewEventKey(key, text, tcell.ModNone))
	if err != nil {
		t.Fatalf("handleKey returned error: %v", err)
	}
	if shouldQuit {
		t.Fatal("handleKey should not quit")
	}
}

func assertEditorText(t *testing.T, app *App, want string) {
	t.Helper()

	if got := app.composerText(); got != want {
		t.Fatalf("editor text = %q, want %q", got, want)
	}
}
