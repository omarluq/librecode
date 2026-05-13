package extension_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/extension"
)

const (
	testBufferComposer       = "composer"
	testContextModeKey       = "mode"
	testEventKey             = "key"
	testEventStartup         = "startup"
	testModeChat             = "chat"
	testUserExtension        = ".librecode/extensions"
	testPathExtensionSource  = "path:.librecode/extensions"
	testRendererDefault      = "default"
	testManagerVimModeSource = "official:vim-mode"
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
			testBufferComposer: testTextBuffer(testBufferComposer, text),
			"transcript":       testTranscriptBuffer(text),
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

func testTextBuffer(name, text string) extension.BufferState {
	return extension.BufferState{
		Metadata: map[string]any{},
		Blocks:   []extension.BufferBlock{},
		Name:     name,
		Text:     text,
		Chars:    stringCharsForTest(text),
		Label:    "",
		Cursor:   len([]rune(text)),
	}
}

func testTranscriptBuffer(text string) extension.BufferState {
	buffer := testTextBuffer("transcript", "")
	buffer.Metadata = map[string]any{"count": 1}
	buffer.Blocks = []extension.BufferBlock{
		{
			Metadata:  map[string]any{},
			CreatedAt: "",
			ID:        "message:0",
			Kind:      "message",
			Role:      "user",
			Text:      text,
			Index:     0,
			Streaming: false,
		},
	}

	return buffer
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
  composer.label = "insert"
  composer.metadata = composer.metadata or {}
  composer.metadata.mode = "vim"
  lc.buf.set("composer", composer)
end)

lc.keymap.set({ role = "composer" }, "x", function(event)
  local composer = lc.buf.get("composer")
  lc.buf.set("composer", {
    text = composer.text .. event.key,
    cursor = composer.cursor + 1,
    label = "normal",
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
			testBufferComposer: testTextBuffer(testBufferComposer, "go"),
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
	assert.Equal(t, "saw go!", result.Buffers["transcript"].Blocks[0].Text)
	assert.Equal(t, "custom", result.Buffers["transcript"].Blocks[0].Role)
}

func TestManager_KeymapAutocmdBufferAndBlockPrimitives(t *testing.T) {
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
  callback = function(event)
    lc.buf.create("scratch", { text = "a\nb\nc", cursor = 1 })
    lc.buf.set_lines("scratch", 1, 2, { "B", "BB" })
    lc.buf.append("scratch", "!")
    lc.buf.set_var("scratch", "seen", true)
    local blocks = lc.buf.get_blocks("transcript", 0, -1)
    lc.buf.append("scratch", ":" .. blocks[1].text .. ":" .. tostring(lc.buf.get_var("scratch", "seen")))
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
	assert.Equal(t, "a\nB\nBB\nc!:go:true", result.Buffers["scratch"].Text)
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
	event.Name = testEventStartup
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
			testBufferComposer: testTextBuffer(testBufferComposer, "1234"),
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

func TestManager_UIPrimitivesUseTerminalWidth(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")

lc.on("render", function()
  local viewport = lc.ui.viewport({ "one", "two", "three", "four" }, 2, 1)
  local list = lc.ui.virtual_list({ { height = 2 }, { height = 3 }, { height = 1 } }, 3, 1)
  local tokens = lc.ui.theme_tokens()
  lc.buf.set_text("metrics", table.concat({
    tostring(lc.ui.measure("語")),
    lc.ui.truncate("abc語", 4),
    lc.ui.pad_right("語", 4),
    table.concat(lc.ui.wrap("aa bb cc", 5), "|"),
    table.concat(viewport.lines, ","),
    tostring(viewport.start) .. ":" .. tostring(viewport["end"]) .. ":" .. tostring(viewport.max_offset),
    tostring(list.items[1].lua_index) .. ":" .. tostring(list.items[1].row_offset) .. ":" .. tostring(list.max_offset),
    tokens[1] .. ":" .. tokens[#tokens],
  }, "\n"))
end)
`)

	event := testTerminalEventWithComposerWindow("", "")
	event.Name = "render"
	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)

	metrics := result.Buffers["metrics"].Text
	assert.Equal(t, "2\nabc…\n語  \naa|bb cc\ntwo,three\n1:3:2\n2:0:3\naccent:warning", metrics)
}

func TestManager_UIDrawLinesSpansRegionAndBox(t *testing.T) {
	t.Parallel()

	manager := loadTestExtension(t, `
local lc = require("librecode")

lc.on("render", function()
  lc.ui.draw_lines("composer", 1, 2, { "one", "two" }, { fg = "muted" })
  lc.ui.draw_spans("composer", 3, 0, {
    { text = "hot", fg = "accent", bold = true },
    { text = " cold", fg = "dim" },
  })
  lc.ui.clear_region("composer", 4, 1, 2, 3, { bg = "muted" })
  lc.ui.draw_box("composer", { fg = "border" })
  lc.ui.draw_batch({
    { window = "composer", kind = "text", row = 5, col = 0, text = "batched", fg = "warning" },
  })
end)
`)

	event := testTerminalEventWithComposerWindow("", "")
	event.Name = "render"
	result, err := manager.HandleTerminalEvent(context.Background(), &event)
	require.NoError(t, err)

	require.Len(t, result.UIDrawOps, 6)
	assert.Equal(t, extension.UIDrawKindText, result.UIDrawOps[0].Kind)
	assert.Equal(t, "one", result.UIDrawOps[0].Text)
	assert.Equal(t, 1, result.UIDrawOps[0].Row)
	assert.Equal(t, extension.UIDrawKindText, result.UIDrawOps[1].Kind)
	assert.Equal(t, extension.UIDrawKindSpans, result.UIDrawOps[2].Kind)
	require.Len(t, result.UIDrawOps[2].Spans, 2)
	assert.Equal(t, "hot", result.UIDrawOps[2].Spans[0].Text)
	assert.True(t, result.UIDrawOps[2].Spans[0].Style.Bold)
	assert.Equal(t, extension.UIDrawKindClear, result.UIDrawOps[3].Kind)
	assert.Equal(t, 4, result.UIDrawOps[3].Row)
	assert.Equal(t, 1, result.UIDrawOps[3].Col)
	assert.Equal(t, 2, result.UIDrawOps[3].Height)
	assert.Equal(t, 3, result.UIDrawOps[3].Width)
	assert.Equal(t, extension.UIDrawKindBox, result.UIDrawOps[4].Kind)
	assert.Equal(t, "batched", result.UIDrawOps[5].Text)
	assert.Equal(t, "warning", result.UIDrawOps[5].Style.FG)
}

func TestLocalLoadPathsParsesPathSources(t *testing.T) {
	t.Parallel()

	paths, err := extension.LocalLoadPaths([]extension.ConfiguredSource{
		{Source: " " + testPathExtensionSource + " ", Version: ""},
		{Source: "path:./custom", Version: ""},
		{Source: testManagerVimModeSource, Version: ""},
		{Source: "github:example/extension", Version: "v1.2.3"},
		{Source: testPathExtensionSource, Version: ""},
	})

	require.NoError(t, err)
	assert.Equal(t, []string{".librecode/extensions", "./custom"}, paths)
}

func TestLocalLoadPathsRejectsUnknownScheme(t *testing.T) {
	t.Parallel()

	_, err := extension.LocalLoadPaths([]extension.ConfiguredSource{{Source: "npm:thing", Version: ""}})

	assert.ErrorContains(t, err, "unsupported source scheme")
}

func TestManager_LoadsDirectoryManifestEntryWithRootModules(t *testing.T) {
	t.Parallel()

	extensionRoot := t.TempDir()
	require.NoError(t, writeTestFile(
		filepath.Join(extensionRoot, "helper.lua"),
		`return { greeting = function(name) return "hello " .. name end }`,
	))
	require.NoError(t, writeTestFile(
		filepath.Join(extensionRoot, "init.lua"),
		`return {
  name = "greeter",
  version = "0.1.0",
  api_version = "v1alpha1",
  entry = "main.lua",
}`,
	))
	require.NoError(t, writeTestFile(
		filepath.Join(extensionRoot, "main.lua"),
		`local helper = require("helper")
return function(librecode)
  librecode.register_command("greet", "Greet", function(args)
    return helper.greeting(args)
  end)
end`,
	))

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadPaths(context.Background(), []string{extensionRoot}))

	result, err := manager.ExecuteCommand(context.Background(), "greet", "lua")
	require.NoError(t, err)
	assert.Equal(t, "hello lua", result)
	require.Len(t, manager.Extensions(), 1)
	assert.Equal(t, "greeter", manager.Extensions()[0].Name)
}

func TestManager_LoadsSymlinkedDirectoryManifest(t *testing.T) {
	t.Parallel()

	extensionRoot := t.TempDir()
	realExtension := filepath.Join(extensionRoot, "real")
	linkedExtension := filepath.Join(extensionRoot, "linked")
	require.NoError(t, os.MkdirAll(realExtension, 0o750))
	require.NoError(t, writeTestFile(
		filepath.Join(realExtension, "init.lua"),
		`return { name = "linked", entry = "main.lua" }`,
	))
	require.NoError(t, writeTestFile(
		filepath.Join(realExtension, "main.lua"),
		`return function(librecode)
  librecode.register_command("linked", "Linked command", function() return "ok" end)
end`,
	))
	createManagerSymlinkOrSkip(t, realExtension, linkedExtension)

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadPaths(context.Background(), []string{extensionRoot}))

	result, err := manager.ExecuteCommand(context.Background(), "linked", "")
	require.NoError(t, err)
	assert.Equal(t, "ok", result)
	require.Len(t, manager.Extensions(), 1)
	assert.Equal(t, "linked", manager.Extensions()[0].Name)
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
	assert.Equal(t, []string{testEventStartup}, loaded[0].Handlers)
	assert.Equal(t, []string{"role:composer:x"}, loaded[0].Keymaps)
	assert.Positive(t, loaded[0].TotalDuration)
}

func TestManager_HasTerminalEventHandlers(t *testing.T) {
	t.Parallel()

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	script := `
librecode.on("startup", function() end)
librecode.keymap.set({ role = "composer" }, "x", function() end)
`
	extensionPath := filepath.Join(t.TempDir(), "handlers.lua")
	require.NoError(t, writeTestFile(extensionPath, script))
	require.NoError(t, manager.LoadPaths(context.Background(), []string{extensionPath}))

	assert.True(t, manager.HasTerminalEventHandlers(testEventStartup))
	assert.True(t, manager.HasTerminalEventHandlers("key"))
	assert.False(t, manager.HasTerminalEventHandlers("render"))
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
			testBufferComposer: testTextBuffer(testBufferComposer, ""),
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
	assert.Equal(t, "insert", startup.Buffers[testBufferComposer].Label)
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
	assert.Equal(t, "normal", result.Buffers[testBufferComposer].Label)
	assert.Equal(t, "vim", result.Buffers[testBufferComposer].Metadata[testContextModeKey])
}

func createManagerSymlinkOrSkip(t *testing.T, oldname, newname string) {
	t.Helper()

	if runtime.GOOS == "windows" {
		t.Skip("skipping symlink test on Windows: symlinks require elevated privileges")
	}
	require.NoError(t, os.Symlink(oldname, newname))
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
