package extension_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	testBufferComposer  = "composer"
	testContextModeKey  = "mode"
	testEventKey        = "key"
	testModeChat        = "chat"
	testRendererDefault = "default"
)

func testLayout(windows map[string]extension.WindowState) extension.LayoutState {
	return extension.LayoutState{Windows: windows, Width: 80, Height: 24}
}

func loadTestExtension(t *testing.T, source string) *extension.Manager {
	t.Helper()

	extensionPath := filepath.Join(t.TempDir(), "runtime.lua")
	require.NoError(t, writeTestFile(extensionPath, source))

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	return manager
}

func testComposerWindow() extension.WindowState {
	return extension.WindowState{
		Metadata:  map[string]any{},
		Name:      testBufferComposer,
		Role:      testBufferComposer,
		Buffer:    testBufferComposer,
		Renderer:  testRendererDefault,
		X:         0,
		Y:         0,
		Width:     80,
		Height:    6,
		CursorRow: 0,
		CursorCol: 2,
		Visible:   true,
	}
}

func testTerminalEventWithComposerWindow(text, key string) extension.TerminalEvent {
	window := testComposerWindow()
	return extension.TerminalEvent{
		Buffers: map[string]extension.BufferState{
			testBufferComposer: {
				Metadata: map[string]any{},
				Name:     testBufferComposer,
				Text:     text,
				Chars:    stringCharsForTest(text),
				Label:    "",
				Cursor:   len([]rune(text)),
			},
		},
		Windows: map[string]extension.WindowState{testBufferComposer: window},
		Layout:  testLayout(map[string]extension.WindowState{testBufferComposer: window}),
		Context: map[string]any{testContextModeKey: testModeChat},
		Data:    map[string]any{},
		Name:    testEventKey,
		Key: extension.ComposerKeyEvent{
			Key:   key,
			Text:  "",
			Ctrl:  key == "ctrl+j",
			Alt:   false,
			Shift: false,
		},
	}
}

func stringCharsForTest(text string) []string {
	chars := make([]string, 0, len([]rune(text)))
	for _, char := range text {
		chars = append(chars, string(char))
	}

	return chars
}

func TestManager_LoadsLuaCommandsToolsAndKeymaps(t *testing.T) {
	t.Parallel()

	const helloExtension = "hello"
	extensionPath := filepath.Join(t.TempDir(), helloExtension+".lua")
	source := `
local lc = require("librecode")

lc.register_command("hello", "Say hello", function(args)
  return "hello " .. args
end)

lc.register_tool("echo", "Echo text", function(args)
  return { content = args.text, details = { seen = true } }
end)

lc.on("startup", function()
  local composer = lc.buf.get("composer")
  composer.label = "vim:INSERT"
  composer.metadata = composer.metadata or {}
  composer.metadata.mode = "vim"
  lc.buf.set("composer", composer)
end)

lc.keymap.set({ role = "composer" }, "x", function(event)
  local composer = lc.buf.get("composer")
  lc.buf.set("composer", {
    text = composer.text .. event.key,
    cursor = composer.cursor + 1,
    label = "vim:NORMAL",
    metadata = { mode = "vim" },
  })
  lc.event.consume()
end, { desc = "append x" })
`
	require.NoError(t, writeTestFile(extensionPath, source))

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	assertLoadedCommand(t, manager.Commands(), helloExtension)
	assertLoadedTool(t, manager.Tools(), helloExtension)
	assertLoadedExtension(t, manager.Extensions())
	assertCommandExecution(t, manager)
	assertToolExecution(t, manager)
	assertTerminalKeyExecution(t, manager)
}

func TestManager_HandleTerminalEventBuffersAndPriority(t *testing.T) {
	t.Parallel()

	extensionPath := filepath.Join(t.TempDir(), "runtime.lua")
	source := `
local lc = require("librecode")

lc.on("key", { priority = 10 }, function(event)
  local composer = lc.buf.get("composer")
  lc.buf.set_text("composer", composer.text .. event.key)
end)

lc.on("key", { priority = 1 }, function()
  local composer = lc.buf.get("composer")
  composer.label = "lua:runtime"
  lc.buf.set("composer", composer)
  lc.buf.append("transcript", { role = "custom", text = "saw " .. composer.text })
  lc.event.consume()
end)
`
	require.NoError(t, writeTestFile(extensionPath, source))

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	event := extension.TerminalEvent{
		Buffers: map[string]extension.BufferState{
			testBufferComposer: {
				Metadata: map[string]any{},
				Name:     testBufferComposer,
				Text:     "go",
				Chars:    []string{"g", "o"},
				Label:    "",
				Cursor:   2,
			},
		},
		Windows: map[string]extension.WindowState{},
		Layout:  testLayout(map[string]extension.WindowState{}),
		Context: map[string]any{},
		Data:    map[string]any{},
		Name:    testEventKey,
		Key: extension.ComposerKeyEvent{
			Key:   "!",
			Text:  "!",
			Ctrl:  false,
			Alt:   false,
			Shift: false,
		},
	}
	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)

	assert.True(t, result.Consumed)
	assert.Equal(t, "go!", result.Buffers[testBufferComposer].Text)
	assert.Equal(t, "lua:runtime", result.Buffers[testBufferComposer].Label)
	require.Len(t, result.Appends, 1)
	assert.Equal(t, extension.BufferAppend{Name: "transcript", Text: "saw go!", Role: "custom"}, result.Appends[0])
}

func TestManager_KeymapAutocmdAndBufferPrimitives(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")

local first_namespace = lc.api.create_namespace("demo")
local second_namespace = lc.api.nvim_create_namespace("demo")
if first_namespace ~= second_namespace then
  error("namespace IDs should be stable")
end

lc.autocmd.create("key", {
  priority = 1,
  callback = function()
    lc.buf.create("scratch", { text = "a\nb\nc", cursor = 1 })
    lc.buf.set_lines("scratch", 1, 2, { "B", "BB" })
    lc.buf.append("scratch", "!")
  end,
})

lc.keymap.set({ buffer = "composer" }, "<c-j>", function()
  local composer = lc.buf.get("composer")
  lc.buf.set("composer", { text = composer.text .. ":mapped", cursor = 99 })
  lc.event.consume()
end, { priority = 10, desc = "map ctrl-j" })
`)

	event := testTerminalEventWithComposerWindow("go", "ctrl+j")
	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)

	assert.True(t, result.Consumed)
	assert.Equal(t, "go:mapped", result.Buffers[testBufferComposer].Text)
	assert.Equal(t, 99, result.Buffers[testBufferComposer].Cursor)
	assert.Equal(t, "a\nB\nBB\nc!", result.Buffers["scratch"].Text)
	assert.Equal(t, []string{"buffer:composer:ctrl+j"}, manager.Extensions()[0].Keymaps)
}

func TestManager_WindowAPIExposesComposerWindow(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")

lc.on("startup", function()
  local win = lc.win.find({ role = "composer" })
  if win == nil then
    error("composer window should exist")
  end
  local composer_buf = lc.win.get_buf(win)
  if composer_buf ~= "composer" then
    error("unexpected composer buffer: " .. tostring(composer_buf))
  end
  local composer = lc.buf.get(composer_buf)
  composer.label = "window-aware"
  lc.buf.set(composer_buf, composer)
end)
`)

	event := testTerminalEventWithComposerWindow("", "")
	event.Context = map[string]any{}
	event.Name = "startup"
	event.Key = extension.ComposerKeyEvent{Key: "", Text: "", Ctrl: false, Alt: false, Shift: false}
	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)
	assert.Equal(t, "window-aware", result.Buffers[testBufferComposer].Label)
}

func TestManager_BufferRangeEditingAndActions(t *testing.T) {
	t.Parallel()

	extensionPath := filepath.Join(t.TempDir(), "runtime.lua")
	source := `
local lc = require("librecode")

lc.on("key", function()
  lc.buf.insert("composer", 0, "A")
  lc.buf.replace("composer", 1, 3, "BC")
  lc.buf.delete_range("composer", 3, 4)
  lc.action.run("history.prev")
  lc.event.consume()
end)
`
	require.NoError(t, writeTestFile(extensionPath, source))

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	event := extension.TerminalEvent{
		Buffers: map[string]extension.BufferState{
			testBufferComposer: {
				Metadata: map[string]any{},
				Name:     testBufferComposer,
				Text:     "1234",
				Chars:    []string{"1", "2", "3", "4"},
				Label:    "",
				Cursor:   4,
			},
		},
		Windows: map[string]extension.WindowState{},
		Layout:  testLayout(map[string]extension.WindowState{}),
		Context: map[string]any{testContextModeKey: testModeChat},
		Data:    map[string]any{},
		Name:    testEventKey,
		Key: extension.ComposerKeyEvent{
			Key:   "!",
			Text:  "!",
			Ctrl:  false,
			Alt:   false,
			Shift: false,
		},
	}
	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)

	assert.True(t, result.Consumed)
	assert.Equal(t, "ABC4", result.Buffers[testBufferComposer].Text)
	require.Len(t, result.Actions, 1)
	assert.Equal(t, extension.ActionCall{Name: "history.prev"}, result.Actions[0])
}

func TestDefaultLoadPathsPrependsOfficialExtensions(t *testing.T) {
	t.Parallel()

	paths := extension.DefaultLoadPaths([]string{".librecode/extensions", "extensions", " custom "})

	assert.Equal(t, []string{"extensions", ".librecode/extensions", "custom"}, paths)
}

func assertLoadedCommand(t *testing.T, commands []extension.Command, extensionName string) {
	t.Helper()

	require.Len(t, commands, 1)
	assert.Equal(t, extension.Command{
		Name:        "hello",
		Description: "Say hello",
		Extension:   extensionName,
	}, commands[0])
}

func assertLoadedTool(t *testing.T, tools []extension.Tool, extensionName string) {
	t.Helper()

	require.Len(t, tools, 1)
	assert.Equal(t, extension.Tool{
		Name:        "echo",
		Description: "Echo text",
		Extension:   extensionName,
	}, tools[0])
}

func assertLoadedExtension(t *testing.T, loaded []extension.LoadedExtension) {
	t.Helper()

	require.Len(t, loaded, 1)
	assert.Equal(t, []string{"role:composer:x"}, loaded[0].Keymaps)
}

func assertCommandExecution(t *testing.T, manager *extension.Manager) {
	t.Helper()

	commandResult, err := manager.ExecuteCommand(context.Background(), "hello", "lua")
	require.NoError(t, err)
	assert.Equal(t, "hello lua", commandResult)
}

func assertToolExecution(t *testing.T, manager *extension.Manager) {
	t.Helper()

	toolResult, err := manager.ExecuteTool(context.Background(), "echo", map[string]any{"text": "ok"})
	require.NoError(t, err)
	assert.Equal(t, "ok", toolResult.Content)
	assert.Equal(t, true, toolResult.Details["seen"])
}

func assertTerminalKeyExecution(t *testing.T, manager *extension.Manager) {
	t.Helper()

	composerWindow := testComposerWindow()
	startupEvent := extension.TerminalEvent{
		Buffers: map[string]extension.BufferState{
			testBufferComposer: {
				Metadata: map[string]any{},
				Name:     testBufferComposer,
				Text:     "",
				Chars:    []string{},
				Label:    "",
				Cursor:   0,
			},
		},
		Windows: map[string]extension.WindowState{testBufferComposer: composerWindow},
		Layout:  testLayout(map[string]extension.WindowState{testBufferComposer: composerWindow}),
		Context: map[string]any{},
		Data:    map[string]any{},
		Name:    "startup",
		Key: extension.ComposerKeyEvent{
			Key:   "",
			Text:  "",
			Ctrl:  false,
			Alt:   false,
			Shift: false,
		},
	}
	startup, err := manager.HandleTerminalEvent(context.Background(), &startupEvent)
	require.NoError(t, err)
	assert.Equal(t, "vim:INSERT", startup.Buffers[testBufferComposer].Label)
	assert.Equal(t, "vim", startup.Buffers[testBufferComposer].Metadata[testContextModeKey])

	resultEvent := extension.TerminalEvent{
		Buffers: map[string]extension.BufferState{
			testBufferComposer: startup.Buffers[testBufferComposer],
		},
		Windows: map[string]extension.WindowState{testBufferComposer: composerWindow},
		Layout:  testLayout(map[string]extension.WindowState{testBufferComposer: composerWindow}),
		Context: map[string]any{testContextModeKey: testModeChat},
		Data:    map[string]any{},
		Name:    testEventKey,
		Key: extension.ComposerKeyEvent{
			Key:   "x",
			Text:  "x",
			Ctrl:  false,
			Alt:   false,
			Shift: false,
		},
	}
	result, err := manager.HandleTerminalEvent(context.Background(), &resultEvent)
	require.NoError(t, err)
	assert.True(t, result.Consumed)
	assert.Equal(t, "x", result.Buffers[testBufferComposer].Text)
	assert.Equal(t, 1, result.Buffers[testBufferComposer].Cursor)
	assert.Equal(t, "vim:NORMAL", result.Buffers[testBufferComposer].Label)
	assert.Equal(t, "vim", result.Buffers[testBufferComposer].Metadata[testContextModeKey])
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
