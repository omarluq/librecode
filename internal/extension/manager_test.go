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

func TestManager_LoadsLuaCommandsToolsAndComposerModes(t *testing.T) {
	t.Parallel()

	const helloExtension = "hello"
	extensionPath := filepath.Join(t.TempDir(), helloExtension+".lua")
	source := `
librecode.register_command("hello", "Say hello", function(args)
  return "hello " .. args
end)

librecode.register_tool("echo", "Echo text", function(args)
  return { content = args.text, details = { seen = true } }
end)

librecode.register_composer_mode("vim", "Vim composer", {
  default = true,
  label = "vim:INSERT",
  on_key = function(event, state)
    if event.key == "x" then
      return { handled = true, text = state.text .. "!", cursor = state.cursor + 1, label = "vim:NORMAL" }
    end
    return { handled = false, label = "vim:INSERT" }
  end,
})
`
	require.NoError(t, writeTestFile(extensionPath, source))

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	assertLoadedCommand(t, manager.Commands(), helloExtension)
	assertLoadedTool(t, manager.Tools(), helloExtension)
	assertLoadedComposerMode(t, manager.ComposerModes(), helloExtension)
	assertLoadedExtension(t, manager.Extensions())
	assertCommandExecution(t, manager)
	assertToolExecution(t, manager)
	assertComposerExecution(t, manager)
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

func assertLoadedComposerMode(t *testing.T, modes []extension.ComposerMode, extensionName string) {
	t.Helper()

	require.Len(t, modes, 1)
	assert.Equal(t, extension.ComposerMode{
		Name:        "vim",
		Description: "Vim composer",
		Extension:   extensionName,
		Label:       "vim:INSERT",
		Default:     true,
	}, modes[0])
}

func assertLoadedExtension(t *testing.T, loaded []extension.LoadedExtension) {
	t.Helper()

	require.Len(t, loaded, 1)
	assert.Equal(t, []string{"vim"}, loaded[0].ComposerModes)
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

func assertComposerExecution(t *testing.T, manager *extension.Manager) {
	t.Helper()

	result, err := manager.HandleComposerKey(
		context.Background(),
		"vim",
		extension.ComposerKeyEvent{
			Key:  "x",
			Text: "x",
			Ctrl: false,
			Alt:  false,
		},
		extension.ComposerState{
			Text:        "ok",
			Chars:       []string{"o", "k"},
			Cursor:      2,
			Working:     false,
			AuthWorking: false,
		},
	)
	require.NoError(t, err)
	assert.Equal(t, extension.ComposerResult{
		Text:      "ok!",
		Label:     "vim:NORMAL",
		Cursor:    3,
		Handled:   true,
		HasText:   true,
		HasCursor: true,
	}, result)
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
