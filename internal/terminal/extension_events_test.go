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
    librecode.buf.set("composer", { text = "lua", cursor = 3, label = "lua:EDIT" })
    librecode.event.consume()
  end
end)
`)

	pressTerminalRune(t, app, "x")

	assertEditorText(t, app, "lua")
	if got, want := app.editor.cursor, 3; got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
	if got, want := app.composer.label, "lua:EDIT"; got != want {
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
	app.editor.setText("from extension")

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

func newExtensionRuntimeTestApp(t *testing.T, source string) *App {
	t.Helper()

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	extensionPath := filepath.Join(t.TempDir(), "runtime.lua")
	require.NoError(t, writeTerminalTestFile(extensionPath, source))
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	app := newRenderTestApp(t)
	app.composer = newComposer("lua", "lua:READY", manager)
	app.extensions = terminalEventRunner(manager)

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
