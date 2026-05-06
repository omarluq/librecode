package tool_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/omarluq/librecode/internal/tool"
)

func TestRegistry_ExecuteJSONRunsBuiltInFileTools(t *testing.T) {
	t.Parallel()

	const (
		pathKey  = "path"
		testPath = "src/main.go"
	)

	ctx := context.Background()
	registry := tool.NewRegistry(t.TempDir())

	writeResult := executeTool(ctx, t, registry, "write", map[string]any{
		pathKey:   testPath,
		"content": "package main\n\nfunc main() {\n\tprintln(\"hello\")\n}\n",
	})
	assert.Contains(t, writeResult.Text(), "Successfully wrote")

	findResult := executeTool(ctx, t, registry, "find", map[string]any{"pattern": "**/*.go"})
	assert.Contains(t, findResult.Text(), "src/main.go")

	grepResult := executeTool(ctx, t, registry, "grep", map[string]any{
		"pattern": "println",
		pathKey:   ".",
		"glob":    "**/*.go",
	})
	assert.Contains(t, grepResult.Text(), "src/main.go:4:")
	assert.Contains(t, grepResult.Text(), "println")

	editResult := executeTool(ctx, t, registry, "edit", map[string]any{
		pathKey: testPath,
		"edits": []map[string]any{
			{"oldText": "hello", "newText": "hola"},
		},
	})
	assert.Contains(t, editResult.Text(), "Successfully replaced")
	assert.Contains(t, editResult.Details["diff"], "hola")

	readResult := executeTool(ctx, t, registry, "read", map[string]any{pathKey: testPath})
	assert.Contains(t, readResult.Text(), "hola")

	lsResult := executeTool(ctx, t, registry, "ls", map[string]any{pathKey: "src"})
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
