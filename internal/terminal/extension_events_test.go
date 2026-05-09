//nolint:testpackage // These tests exercise unexported terminal extension event helpers.
package terminal

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
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

	pressTerminalKey(t, app, tcell.KeyRune, "x")

	assertEditorText(t, app, "lua")
	if got, want := app.composerCursor(), 1; got != want {
		t.Fatalf("cursor = %d, want %d", got, want)
	}
	if got, want := app.composerLabel(), "lua:EDIT"; got != want {
		t.Fatalf("composer label = %s, want %s", got, want)
	}
}

func TestExtensionPromptSubmitCanConsumeDefault(t *testing.T) {
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
	if got := len(app.messages); got != 0 {
		t.Fatalf("host messages = %d, want 0", got)
	}
	transcript := app.extensionRuntimeBuffers[extensionBufferTranscript]
	require.Len(t, transcript.Blocks, 1)
	if got, want := transcript.Blocks[0].Text, "handled: from extension"; got != want {
		t.Fatalf("transcript block = %q, want %q", got, want)
	}
}

func TestExtensionRuntimeBuffersPersistBetweenEvents(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.keymap.set({ buffer = "composer" }, "x", function()
  local scratch = librecode.buf.get("scratch")
  librecode.buf.set_text("scratch", scratch.text .. "x")
  librecode.event.consume()
end)
`)

	pressTerminalKey(t, app, tcell.KeyRune, "x")
	pressTerminalKey(t, app, tcell.KeyRune, "x")

	buffer, ok := app.extensionRuntimeBuffers["scratch"]
	if !ok {
		t.Fatal("scratch buffer should persist")
	}
	if got, want := buffer.Text, "xx"; got != want {
		t.Fatalf("scratch buffer = %q, want %q", got, want)
	}
}

func TestExtensionEventsExposeTranscriptThinkingAndToolBuffers(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(database.RoleUser, "hello")
	app.addMessage(database.RoleThinking, "because")
	app.addMessage(database.RoleToolResult, "tool output")
	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptDelta, "answer"))

	buffers := app.extensionBuffers()
	for _, name := range []string{
		extensionBufferTranscript,
		extensionBufferThinking,
		extensionBufferTools,
	} {
		if _, ok := buffers[name]; !ok {
			t.Fatalf("expected %s buffer", name)
		}
	}
	assertBufferCount(t, buffers, extensionBufferTranscript, 3)
	assertBufferCount(t, buffers, extensionBufferThinking, 1)
	assertBufferCount(t, buffers, extensionBufferTools, 1)
	if got := buffers[extensionBufferTranscript].Metadata["snapshot_count"]; got != 4 {
		t.Fatalf("snapshot count = %v, want 4", got)
	}
	if got := buffers[extensionBufferTranscript].Text; got != "" {
		t.Fatalf("default transcript buffer text = %q, want empty projection", got)
	}
}

func TestExtensionEventExposesStructuredTranscriptBuffer(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(database.RoleUser, "hello")
	app.addMessage(database.RoleThinking, "because")
	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptDelta, "answer"))

	event := app.newExtensionEvent(extensionEventRender, emptyExtensionKeyEvent())
	transcript := event.Buffers[extensionBufferTranscript]
	if got, want := transcript.Metadata["snapshot_count"], 3; got != want {
		t.Fatalf("transcript count = %v, want %d", got, want)
	}
	if got, want := len(transcript.Blocks), 3; got != want {
		t.Fatalf("transcript blocks = %d, want %d", got, want)
	}
	assertTranscriptBlock(t, &transcript.Blocks[0], database.RoleUser, "hello", false)
	assertTranscriptBlock(t, &transcript.Blocks[1], database.RoleThinking, "because", false)
	assertTranscriptBlock(t, &transcript.Blocks[2], database.RoleAssistant, "answer", true)
}

func TestExtensionCanOverrideTranscriptBufferRendering(t *testing.T) {
	t.Parallel()

	app := newRenderTestApp(t)
	app.addMessage(database.RoleUser, "host transcript")
	app.applyExtensionBuffer(extensionBufferTranscript, &extension.BufferState{
		Metadata: map[string]any{},
		Blocks:   []extension.BufferBlock{},
		Name:     extensionBufferTranscript,
		Text:     "lua transcript",
		Label:    "",
		Chars:    []string{"l", "u", "a"},
		Cursor:   14,
	})
	layout := app.defaultRuntimeLayout(40, 12)
	app.frame = newCellBuffer(layout.Width, layout.Height, tcell.StyleDefault)

	app.drawTranscriptWindow(&layout)

	text := frameText(app.frame)
	if !strings.Contains(text, "lua transcript") {
		t.Fatalf("expected extension transcript buffer render, frame = %q", text)
	}
	if strings.Contains(text, "host transcript") {
		t.Fatalf("host transcript should be hidden by buffer override, frame = %q", text)
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
  composer.renderer = "extension"
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
	if got, want := composer.Renderer, "extension"; got != want {
		t.Fatalf("composer renderer = %q, want %q", got, want)
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
librecode.on("resize", function(event)
  librecode.buf.set_text("status", "resized " .. event.data.width .. "x" .. event.data.height)
end)
`)

	require.NoError(t, app.handleResizeExtensions(context.Background()))

	if got, want := app.statusMessage, "resized 80x24"; got != want {
		t.Fatalf("status = %q, want %q", got, want)
	}
}

func TestExtensionModelAndToolLifecycleEvents(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("model_delta", function(event)
  librecode.buf.append("events", "model:" .. event.data.text .. "\n")
end)
librecode.on("thinking_delta", function(event)
  librecode.buf.append("events", "thinking:" .. event.data.text .. "\n")
end)
librecode.on("tool_start", function(event)
  librecode.buf.append("events", "tool_start:" .. event.data.name .. "\n")
end)
librecode.on("tool_end", function(event)
  librecode.buf.append("events", "tool_end:" .. event.data.name .. ":" .. event.data.result .. "\n")
end)
`)

	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptDelta, "hello"))
	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptThinkingDelta, "why"))
	app.handlePromptStreamEvent(context.Background(), newTestAsyncEvent(asyncEventPromptToolStart, "bash"))
	toolEvent := newTestAsyncEvent(asyncEventPromptToolResult, "")
	toolEvent.ToolEvent = newTestToolEvent("bash", "ok")
	app.handlePromptStreamEvent(context.Background(), toolEvent)

	buffer := app.extensionRuntimeBuffers["events"]
	want := "model:hello\nthinking:why\ntool_start:bash\ntool_end:bash:ok\n"
	if got := buffer.Text; got != want {
		t.Fatalf("events buffer = %q, want %q", got, want)
	}
}

func assertBufferCount(t *testing.T, buffers map[string]extension.BufferState, name string, want int) {
	t.Helper()

	buffer := buffers[name]
	got, ok := buffer.Metadata[extensionMetadataCount].(int)
	if !ok {
		t.Fatalf("buffer %s count metadata = %#v, want int", name, buffer.Metadata[extensionMetadataCount])
	}
	if got != want {
		t.Fatalf("buffer %s count = %d, want %d", name, got, want)
	}
}

func assertTranscriptBlock(
	t *testing.T,
	block *extension.BufferBlock,
	role database.Role,
	text string,
	streaming bool,
) {
	t.Helper()
	if got := block.Role; got != string(role) {
		t.Fatalf("block role = %s, want %s", got, role)
	}
	if got := block.Text; got != text {
		t.Fatalf("block text = %q, want %q", got, text)
	}
	if got := block.Streaming; got != streaming {
		t.Fatalf("block streaming = %v, want %v", got, streaming)
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

func TestExtensionTimerDeferRunsOnNextRuntimeEvent(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("startup", function()
  librecode.timer.defer(0, function()
    librecode.buf.append("events", "timer\n")
  end)
end)
`)

	require.NoError(t, app.emitExtensionRuntimeEvent(context.Background(), extensionEventTick, map[string]any{}))

	buffer := app.extensionRuntimeBuffers["events"]
	if got, want := buffer.Text, "timer\n"; got != want {
		t.Fatalf("events buffer = %q, want %q", got, want)
	}
}

func TestExtensionTimerStopCancelsDeferredTimer(t *testing.T) {
	t.Parallel()

	app := newExtensionRuntimeTestApp(t, `
librecode.on("startup", function()
  local id = librecode.timer.defer(0, function()
    librecode.buf.append("events", "timer\n")
  end)
  librecode.timer.stop(id)
end)
`)

	require.NoError(t, app.emitExtensionRuntimeEvent(context.Background(), extensionEventTick, map[string]any{}))

	if _, ok := app.extensionRuntimeBuffers["events"]; ok {
		t.Fatal("stopped timer should not mutate events buffer")
	}
}

func TestTerminalKeyEventFallsBackToTcellName(t *testing.T) {
	t.Parallel()

	keyEvent := terminalKeyEvent(tcell.NewEventKey(tcell.KeyF1, "", tcell.ModNone))

	if keyEvent.Key == "" {
		t.Fatal("key event name should not be empty")
	}
}
