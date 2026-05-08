//nolint:testpackage // These tests exercise unexported terminal extension event helpers.
package terminal

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/gdamore/tcell/v3"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/database"
	"github.com/omarluq/librecode/internal/extension"
)

func TestExtensionKeyCanMutateComposerAndConsumeDefault(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("key", function(event)
  if event.key == "x" then
    librecode.buf.set("composer", { text = "lua", cursor = 1, label = "lua:EDIT" })
    librecode.event.consume()
  end
end)
`)

	pressTerminalRune(t, app, "x")

	assertEditorText(t, app, "lua")
	if got, want := app.composerCursor(), 1; got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
	if got, want := app.composerLabel(), "lua:EDIT"; got != want {
		t.Fatalf("composer label = %s, want %s", got, want)
	}
}

func TestExtensionPromptSubmitCanAppendTranscriptAndConsumeDefault(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("prompt_submit", function()
  local composer = librecode.buf.get("composer")
  librecode.buf.append("transcript", { role = "custom", text = "handled: " .. composer.text })
  librecode.buf.set_text("composer", "")
  librecode.event.consume()
end)
`)
	app.setComposerText("from extension")

	shouldQuit, err := app.submit(context.Background())
	require.NoError(t, err)
	if shouldQuit {
		t.Fatal("submit should not quit")
	}

	assertEditorText(t, app, "")
	require.Len(t, app.messages, 1)
	if got, want := app.messages[0].Role, database.RoleCustom; got != want {
		t.Fatalf("message role = %s, want %s", got, want)
	}
	if got, want := app.messages[0].Content, "handled: from extension"; got != want {
		t.Fatalf("message = %q, want %q", got, want)
	}
}

func TestExtensionRuntimeBuffersPersistBetweenEvents(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.keymap.set("composer", "x", function()
  local scratch = librecode.buf.get("scratch")
  librecode.buf.set_text("scratch", scratch.text .. "x")
  librecode.event.consume()
end)
`)

	pressTerminalRune(t, app, "x")
	pressTerminalRune(t, app, "x")

	buffer, ok := app.extensionRuntimeBuffers["scratch"]
	if !ok {
		t.Fatal("scratch buffer should persist")
	}
	if got, want := buffer.Text, "xx"; got != want {
		t.Fatalf("scratch buffer = %q, want %q", got, want)
	}
}

func TestExtensionActionHistoryPrevRestoresPrompt(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("key", function(event)
  if event.key == "f1" then
    librecode.action.run("history.prev")
    librecode.event.consume()
  end
end)
`)
	app.recordPromptHistory("first")
	app.recordPromptHistory("second")

	shouldQuit, err := app.handleKey(context.Background(), tcell.NewEventKey(tcell.KeyF1, "", tcell.ModNone))
	require.NoError(t, err)
	if shouldQuit {
		t.Fatal("handleKey should not quit")
	}
	assertEditorText(t, app, "second")
}

func TestExtensionRenderEventCanDrawAndSetLayout(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("render", function()
  local layout = librecode.layout.get()
  local composer = layout.windows.composer
  composer.y = 1
  composer.height = 4
  layout.windows.composer = composer
  librecode.layout.set(layout)
  librecode.ui.clear_window("composer")
  librecode.ui.draw_text("composer", 0, 0, "lua composer", { fg = "accent", bold = true })
  librecode.ui.set_cursor("composer", 1, 2)
end)
`)
	layout := app.currentRuntimeLayout()

	app.runRenderExtensions(context.Background(), &layout)

	if app.runtimeLayout == nil {
		t.Fatal("render extension should set runtime layout")
	}
	composer := app.runtimeLayout.Windows[extensionBufferComposer]
	if got, want := composer.Y, 1; got != want {
		t.Fatalf("composer y = %d, want %d", got, want)
	}
	if got, want := composer.Height, 4; got != want {
		t.Fatalf("composer height = %d, want %d", got, want)
	}
	override := app.uiWindowOverrides[extensionBufferComposer]
	if !override.Reset {
		t.Fatal("composer window should be cleared")
	}
	require.Len(t, override.DrawOps, 1)
	if got, want := override.DrawOps[0].Text, "lua composer"; got != want {
		t.Fatalf("draw text = %q, want %q", got, want)
	}
	if app.uiCursor == nil {
		t.Fatal("render extension should set cursor")
	}
	if got, want := app.uiCursor.Row, 1; got != want {
		t.Fatalf("cursor row = %d, want %d", got, want)
	}
}

func TestExtensionResizeEventCanMutateStatus(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("resize", function()
  librecode.buf.set_text("status", "resized")
end)
`)

	require.NoError(t, app.handleResizeExtensions(context.Background()))

	if got, want := app.statusMessage, "resized"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func newExtensionRuntimeTestApp(t *testing.T, source string) *App {
	t.Helper()

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	extensionPath := filepath.Join(t.TempDir(), "runtime.lua")
	require.NoError(t, writeTerminalTestFile(extensionPath, source))
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	app := newRenderTestApp(t)
	app.extensions = manager
	require.NoError(t, app.runStartupExtensions(context.Background()))

	return app
}

func writeTerminalTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}

func TestTerminalKeyEventFallsBackToTcellName(t *testing.T) {
	t.Parallel()

	keyEvent := terminalKeyEvent(tcell.NewEventKey(tcell.KeyF1, "", tcell.ModNone))

	if keyEvent.Key == "" {
		t.Fatal("key event name should not be empty")
	}
}
