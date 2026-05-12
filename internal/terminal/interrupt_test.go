//nolint:testpackage // These tests exercise unexported terminal interrupt helpers.
package terminal

import (
	"context"
	"testing"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/database"
)

const interruptTestPrompt = "hello"

func TestDoubleEscapeInterruptsWorkingPrompt(t *testing.T) {
	t.Parallel()

	canceled := false
	app := newInterruptTestApp(t, func() { canceled = true })

	pressTerminalKey(t, app, tcell.KeyEscape, "")
	if canceled {
		t.Fatal("first escape should not cancel")
	}
	if !app.working {
		t.Fatal("first escape should keep prompt working")
	}

	pressTerminalKey(t, app, tcell.KeyEscape, "")
	assertPromptCanceled(t, app, canceled)
}

func TestAltEscapeInterruptsWorkingPrompt(t *testing.T) {
	t.Parallel()

	canceled := false
	app := newInterruptTestApp(t, func() { canceled = true })

	shouldQuit, err := app.handleKey(
		context.Background(),
		tcell.NewEventKey(tcell.KeyEscape, "", tcell.ModAlt),
	)
	if err != nil {
		t.Fatalf("handleKey returned error: %v", err)
	}
	if shouldQuit {
		t.Fatal("Alt+Escape should not quit")
	}
	assertPromptCanceled(t, app, canceled)
}

func TestKeyEscInterruptsWorkingPrompt(t *testing.T) {
	t.Parallel()

	canceled := false
	app := newInterruptTestApp(t, func() { canceled = true })

	pressTerminalKey(t, app, tcell.KeyEsc, "")
	pressTerminalKey(t, app, tcell.KeyEsc, "")
	assertPromptCanceled(t, app, canceled)
}

func TestWorkingEscapeSequenceResetsAfterEditorInput(t *testing.T) {
	t.Parallel()

	canceled := false
	app := newInterruptTestApp(t, func() { canceled = true })

	pressTerminalKey(t, app, tcell.KeyEscape, "")
	pressTerminalKey(t, app, tcell.KeyRune, "x")
	pressTerminalKey(t, app, tcell.KeyEscape, "")
	if canceled {
		t.Fatal("escape sequence should reset after editor input")
	}
}

func TestHandleEscapeOpensTreeWhenIdleComposerEmpty(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	pressTerminalKey(t, app, tcell.KeyEscape, "")
	if app.mode != modeChat {
		t.Fatal("first escape should not open tree")
	}
	pressTerminalKey(t, app, tcell.KeyEscape, "")
	if app.mode != modePanel {
		t.Fatal("second idle escape should open tree panel")
	}
}

func assertPromptCanceled(t *testing.T, app *App, canceled bool) {
	t.Helper()

	if !canceled {
		t.Fatal("expected active prompt cancellation")
	}
	if app.working {
		t.Fatal("cancellation should stop working state")
	}
	if got := app.composerText(); got != interruptTestPrompt {
		t.Fatalf("composer text = %q, want restored prompt", got)
	}
}

func newInterruptTestApp(t *testing.T, cancel context.CancelFunc) *App {
	t.Helper()

	app := newRenderTestApp(t)
	app.working = true
	app.activePrompt = newTestActivePrompt(cancel)
	app.addMessage(database.RoleUser, interruptTestPrompt)

	return app
}

func newTestActivePrompt(cancel context.CancelFunc) *activePromptState {
	return &activePromptState{
		Cancel:           cancel,
		ParentEntryID:    nil,
		SessionID:        "",
		UserEntryID:      "",
		Prompt:           interruptTestPrompt,
		ID:               1,
		BaselineMessages: 0,
		Canceled:         false,
	}
}
