//nolint:testpackage // These tests exercise unexported terminal input helpers.
package terminal

import (
	"context"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

const vimInsertLabel = "vim:INSERT"

func TestVimComposerStartsInInsertMode(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)
	pressTerminalRune(t, app, "h")

	assertEditorText(t, app, "h")
	if got, want := app.composerLabel(), vimInsertLabel; got != want {
		t.Fatalf("vim label = %s, want %s", got, want)
	}
}

func TestVimComposerNormalModeMotionsAndDelete(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)
	app.setComposerText("hello")

	pressTerminalKey(t, app, tcell.KeyEscape, "")
	pressTerminalRune(t, app, "h")
	pressTerminalRune(t, app, "x")

	assertEditorText(t, app, "helo")
	if got, want := app.composerCursor(), 3; got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
}

func TestVimComposerChangeWordReturnsToInsert(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)
	app.setComposerText("hello world")
	pressTerminalKey(t, app, tcell.KeyEscape, "")
	pressTerminalRune(t, app, "0")
	pressTerminalRune(t, app, "c")
	pressTerminalRune(t, app, "w")
	pressTerminalRune(t, app, "y")
	pressTerminalRune(t, app, "o")

	assertEditorText(t, app, "yo world")
	if got, want := app.composerLabel(), vimInsertLabel; got != want {
		t.Fatalf("vim label = %s, want %s", got, want)
	}
}

func TestVimComposerLineDeletePasteAndUndo(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)
	app.setComposerText("one\ntwo\nthree")
	pressTerminalKey(t, app, tcell.KeyEscape, "")
	pressTerminalRune(t, app, "g")
	pressTerminalRune(t, app, "g")
	pressTerminalRune(t, app, "d")
	pressTerminalRune(t, app, "d")
	assertEditorText(t, app, "two\nthree")

	pressTerminalRune(t, app, "p")
	assertEditorText(t, app, "tone\nwo\nthree")

	pressTerminalRune(t, app, "u")
	assertEditorText(t, app, "two\nthree")
}

func TestVimComposerBorderShowsMode(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)

	layout := app.composerLayout(80, 24)
	if len(layout.editor.Lines) == 0 || !strings.HasSuffix(layout.editor.Lines[0].Text, "vim:INSERT──╮") {
		t.Fatalf("editor border = %q, want vim mode at end", layout.editor.Lines[0].Text)
	}
	lines := app.footerLines(80)
	if len(lines) < 2 || containsText(lines[1].Text, vimInsertLabel) {
		t.Fatalf("footer line = %q, want no vim mode", lines[1].Text)
	}
}

func TestVimComposerInsertModeOwnsSubmitAndNewline(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)
	app.setComposerText("hello")

	pressTerminalShiftEnter(t, app)
	assertEditorText(t, app, "hello\n")
	if app.composerLabel() != vimInsertLabel {
		t.Fatalf("label = %q, want %q", app.composerLabel(), vimInsertLabel)
	}
}

func newVimTestApp(t *testing.T) *App {
	t.Helper()

	manager := newVimExtensionManager(t)
	app := newRenderTestApp(t)
	app.extensions = manager
	require.NoError(t, app.runStartupExtensions(context.Background()))

	return app
}

func newVimExtensionManager(t *testing.T) *extension.Manager {
	t.Helper()

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)

	extensionPath := filepath.Join("..", "..", "extensions", "vim-mode.lua")
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	loaded := manager.Extensions()
	require.Len(t, loaded, 1)
	require.NotEmpty(t, loaded[0].Keymaps)

	return manager
}

func pressTerminalRune(t *testing.T, app *App, value string) {
	t.Helper()

	shouldQuit, err := app.handleKey(context.Background(), tcell.NewEventKey(tcell.KeyRune, value, tcell.ModNone))
	if err != nil {
		t.Fatalf("handleKey returned error: %v", err)
	}
	if shouldQuit {
		t.Fatal("handleKey should not quit")
	}
}

func pressTerminalShiftEnter(t *testing.T, app *App) {
	t.Helper()

	shouldQuit, err := app.handleKey(context.Background(), tcell.NewEventKey(tcell.KeyEnter, "", tcell.ModShift))
	if err != nil {
		t.Fatalf("handleKey returned error: %v", err)
	}
	if shouldQuit {
		t.Fatal("handleKey should not quit")
	}
}

func containsText(text, needle string) bool {
	return strings.Contains(text, needle)
}
