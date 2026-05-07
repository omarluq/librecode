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
	if got, want := app.composer.label, vimInsertLabel; got != want {
		t.Fatalf("vim label = %s, want %s", got, want)
	}
}

func TestVimComposerNormalModeMotionsAndDelete(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)
	app.editor.setText("hello")

	pressTerminalKey(t, app, tcell.KeyEscape, "")
	pressTerminalRune(t, app, "h")
	pressTerminalRune(t, app, "x")

	assertEditorText(t, app, "helo")
	if got, want := app.editor.cursor, 3; got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
}

func TestVimComposerChangeWordReturnsToInsert(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)
	app.editor.setText("hello world")
	pressTerminalKey(t, app, tcell.KeyEscape, "")
	pressTerminalRune(t, app, "0")
	pressTerminalRune(t, app, "c")
	pressTerminalRune(t, app, "w")
	pressTerminalRune(t, app, "y")
	pressTerminalRune(t, app, "o")

	assertEditorText(t, app, "yo world")
	if got, want := app.composer.label, vimInsertLabel; got != want {
		t.Fatalf("vim label = %s, want %s", got, want)
	}
}

func TestVimComposerLineDeletePasteAndUndo(t *testing.T) {
	t.Parallel()

	app := newVimTestApp(t)
	app.editor.setText("one\ntwo\nthree")
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
	if len(layout.editor.Lines) == 0 || !strings.HasSuffix(layout.editor.Lines[0].Text, "insert──╮") {
		t.Fatalf("editor border = %q, want vim mode at end", layout.editor.Lines[0].Text)
	}
	lines := app.footerLines(80)
	if len(lines) < 2 || containsText(lines[1].Text, vimInsertLabel) {
		t.Fatalf("footer line = %q, want no vim mode", lines[1].Text)
	}
}

func newVimTestApp(t *testing.T) *App {
	t.Helper()

	manager := newVimExtensionManager(t)
	mode := manager.ComposerModes()[0]
	app := newRenderTestApp(t)
	app.composer = newComposer(mode.Name, mode.Label, manager)
	app.extensions = terminalEventRunner(manager)

	return app
}

func newVimExtensionManager(t *testing.T) *extension.Manager {
	t.Helper()

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)

	extensionPath := filepath.Join("..", "..", "extensions", "vim-mode.lua")
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	modes := manager.ComposerModes()
	require.Len(t, modes, 1)
	require.Equal(t, extension.ComposerMode{
		Name:        "vim",
		Description: "Full Vim mode for the chat composer",
		Extension:   "vim-mode",
		Label:       vimInsertLabel,
		Default:     true,
	}, modes[0])

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

func containsText(text, needle string) bool {
	return strings.Contains(text, needle)
}
