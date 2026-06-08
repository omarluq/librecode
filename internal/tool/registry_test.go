package tool_test

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

const (
	registryTestContentKey = "content"
	registryTestPathKey    = "path"
)

func TestRegistry_ExecuteJSONRunsBuiltInFileTools(t *testing.T) {
	t.Parallel()

	const testPath = "src/main.go"

	ctx := context.Background()
	registry := tool.NewRegistry(t.TempDir())

	writeResult := executeTool(ctx, t, registry, "write", map[string]any{
		registryTestPathKey:    testPath,
		registryTestContentKey: "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n",
	})
	assert.Contains(t, writeResult.Text(), "Successfully wrote")

	findResult := executeTool(ctx, t, registry, "find", map[string]any{"pattern": "**/*.go"})
	assert.Contains(t, findResult.Text(), "src/main.go")

	grepResult := executeTool(ctx, t, registry, "grep", map[string]any{
		"pattern":           "println",
		registryTestPathKey: ".",
		"glob":              "**/*.go",
	})
	assert.Contains(t, grepResult.Text(), "src/main.go:4:")
	assert.Contains(t, grepResult.Text(), "println")

	editResult := executeTool(ctx, t, registry, "edit", map[string]any{
		registryTestPathKey: testPath,
		"edits": []map[string]any{
			{"oldText": "hello", "newText": "hola"},
		},
	})
	assert.Contains(t, editResult.Text(), "Successfully replaced")
	assert.Contains(t, editResult.Details["diff"], "hola")

	readResult := executeTool(ctx, t, registry, "read", map[string]any{registryTestPathKey: testPath})
	assert.Contains(t, readResult.Text(), "hola")

	lsResult := executeTool(ctx, t, registry, "ls", map[string]any{registryTestPathKey: "src"})
	assert.Equal(t, "main.go", lsResult.Text())
}

func TestRegistry_ExecuteJSONRunsBashInWorkingDirectory(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	result := executeTool(context.Background(), t, registry, "bash", map[string]any{
		"command": "pwd && printf ok",
	})

	assert.Contains(t, result.Text(), "ok")
}

func TestRegistry_RegisterRejectsDuplicateTool(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())

	err := registry.Register(tool.NewReadTool(t.TempDir()))

	require.Error(t, err)
	assert.True(t, errors.Is(err, tool.ErrDuplicateTool))
}

func TestRegistry_Metadata(t *testing.T) {
	t.Parallel()

	cwd := t.TempDir()
	registry := tool.NewRegistry(cwd)

	assert.Equal(t, cwd, registry.CWD())
	assert.NotEmpty(t, registry.Definitions())
	assert.Len(t, tool.AllDefinitions(), len(registry.Definitions()))
}

func TestRegistry_ExecuteJSONValidatesRequiredArgumentsBeforeExecution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		input   map[string]any
		name    string
		tool    string
		missing string
	}{
		{name: "bash command", tool: "bash", input: map[string]any{}, missing: "command"},
		{
			name:    "write content",
			tool:    "write",
			input:   map[string]any{registryTestPathKey: "empty.txt"},
			missing: registryTestContentKey,
		},
	}
	registry := tool.NewRegistry(t.TempDir())
	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			payload, err := json.Marshal(testCase.input)
			require.NoError(t, err)

			_, err = registry.ExecuteJSON(context.Background(), testCase.tool, payload)

			require.Error(t, err)
			assert.Contains(t, err.Error(), testCase.missing+" is required")
			assert.Contains(t, err.Error(), "call "+testCase.tool+" with")
		})
	}
}

func TestRegistry_ExecuteJSONAllowsIntentionalEmptyWriteContent(t *testing.T) {
	t.Parallel()

	registry := tool.NewRegistry(t.TempDir())
	result := executeTool(context.Background(), t, registry, "write", map[string]any{
		registryTestPathKey:    "empty.txt",
		registryTestContentKey: "",
	})

	assert.Contains(t, result.Text(), "Successfully wrote 0 bytes")
}

func executeTool(
	ctx context.Context,
	t *testing.T,
	registry *tool.Registry,
	name string,
	input map[string]any,
) tool.Result {
	t.Helper()

	payload, err := json.Marshal(input)
	require.NoError(t, err)

	result, err := registry.ExecuteJSON(ctx, name, payload)
	require.NoError(t, err)

	return result
}
