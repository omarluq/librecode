//nolint:testpackage // These tests exercise unexported terminal interrupt helpers.
package terminal

import (
	"context"
	"testing"

	"github.com/gdamore/tcell/v3"

	"github.com/omarluq/librecode/internal/transcript"
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

func TestForceExitRequiresDoubleControlC(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	if app.handleForceExit() {
		t.Fatal("first Ctrl+C should not quit")
	}
	if got, want := app.statusMessage, "press Ctrl+C again to exit"; got != want {
		t.Fatalf("statusMessage = %q, want %q", got, want)
	}
	if !app.handleForceExit() {
		t.Fatal("second Ctrl+C should quit")
	}
}

func TestHandleEscape(t *testing.T) {
	t.Parallel()

	tests := []struct {
		setup      func(*App)
		assert     func(*testing.T, *App)
		name       string
		pressCount int
	}{
		{
			name:       "clears composer text",
			pressCount: 1,
			setup:      func(app *App) { app.setComposerText("draft") },
			assert: func(t *testing.T, app *App) {
				t.Helper()
				if got := app.composerText(); got != "" {
					t.Fatalf("composer text = %q, want empty", got)
				}
				if got, want := app.statusMessage, "editor cleared"; got != want {
					t.Fatalf("statusMessage = %q, want %q", got, want)
				}
			},
		},
		{
			name:       "opens tree on double escape",
			pressCount: 2,
			setup:      func(*App) {},
			assert: func(t *testing.T, app *App) {
				t.Helper()
				if app.mode != modePanel {
					t.Fatal("second idle escape should open tree panel")
				}
			},
		},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			app := newRenderTestApp(t)
			testCase.setup(app)
			for range testCase.pressCount {
				pressTerminalKey(t, app, tcell.KeyEscape, "")
			}
			testCase.assert(t, app)
		})
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
	app.addMessage(transcript.RoleUser, interruptTestPrompt)

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
