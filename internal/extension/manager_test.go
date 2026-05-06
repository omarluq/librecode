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

func TestManager_LoadsLuaCommandsAndTools(t *testing.T) {
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
`
	require.NoError(t, writeTestFile(extensionPath, source))

	manager := extension.NewManager(slog.New(slog.NewTextHandler(io.Discard, nil)))
	t.Cleanup(manager.Shutdown)
	require.NoError(t, manager.LoadFile(context.Background(), extensionPath))

	commands := manager.Commands()
	require.Len(t, commands, 1)
	assert.Equal(t, extension.Command{
		Name:        helloExtension,
		Description: "Say hello",
		Extension:   helloExtension,
	}, commands[0])

	tools := manager.Tools()
	require.Len(t, tools, 1)
	assert.Equal(t, extension.Tool{Name: "echo", Description: "Echo text", Extension: helloExtension}, tools[0])

	commandResult, err := manager.ExecuteCommand(context.Background(), "hello", "lua")
	require.NoError(t, err)
	assert.Equal(t, "hello lua", commandResult)

	toolResult, err := manager.ExecuteTool(context.Background(), "echo", map[string]any{"text": "ok"})
	require.NoError(t, err)
	assert.Equal(t, "ok", toolResult.Content)
	assert.Equal(t, true, toolResult.Details["seen"])
}

func writeTestFile(path, content string) error {
	return os.WriteFile(path, []byte(content), 0o600)
}
