package plugin_test

import (
	"context"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/plugin"
)

func TestManager_LoadsLuaCommandsAndTools(t *testing.T) {
	t.Parallel()

	pluginPath := filepath.Join(t.TempDir(), "hello.lua")
	source := `
librecode.register_command("hello", "Say hello", function(args)
  return "hello " .. args
end)

librecode.register_tool("echo", "Echo text", function(args)
  return { content = args.text, details = { seen = true } }
end)
`
	require.NoError(t, writeTestFile(pluginPath, source))

	manager := plugin.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadFile(context.Background(), pluginPath))

	commands := manager.Commands()
	require.Len(t, commands, 1)
	assert.Equal(t, plugin.Command{Name: "hello", Description: "Say hello", Plugin: "hello"}, commands[0])

	tools := manager.Tools()
	require.Len(t, tools, 1)
	assert.Equal(t, plugin.Tool{Name: "echo", Description: "Echo text", Plugin: "hello"}, tools[0])

	commandResult, err := manager.ExecuteCommand(context.Background(), "hello", "lua")
	require.NoError(t, err)
	assert.Equal(t, "hello lua", commandResult)

	toolResult, err := manager.ExecuteTool(context.Background(), "echo", map[string]any{"text": "ok"})
	require.NoError(t, err)
	assert.Equal(t, "ok", toolResult.Content)
	assert.Equal(t, true, toolResult.Details["seen"])
}

func writeTestFile(path string, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
